package s3

import (
	"OpenSPMRegistry/config"
	"OpenSPMRegistry/mimetypes"
	"OpenSPMRegistry/models"
	"OpenSPMRegistry/repo"
	"OpenSPMRegistry/repo/files"
	"OpenSPMRegistry/utils"
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Repo implements the repo.Repo interface backed by an S3 (or S3-compatible) bucket.
// Object keys mirror the on-disk layout used by FileRepo: <prefix>/<scope>/<name>/<version>/<filename>.
type S3Repo struct {
	repo.Access
	client       s3API
	config       config.S3Config
	timeProvider utils.TimeProvider
}

// NewS3Repo creates a new S3-backed repository.
func NewS3Repo(ctx context.Context, cfg config.S3Config) (*S3Repo, error) {
	client, err := newS3Client(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	return &S3Repo{
		Access:       newAccess(client, cfg),
		client:       client,
		config:       cfg,
		timeProvider: utils.NewRealTimeProvider(),
	}, nil
}

// ExtractManifestFiles extracts Package.swift and Package.json from the source archive.
func (r *S3Repo) ExtractManifestFiles(ctx context.Context, element *models.UploadElement) error {
	if element.MimeType != mimetypes.ApplicationZip {
		return errors.New("unsupported mime type")
	}

	reader, err := r.GetReader(ctx, element)
	if err != nil {
		return fmt.Errorf("failed to get source archive: %w", err)
	}
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read source archive: %w", err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}

	fileExtractor := func(name string, rc io.ReadCloser) error {
		defer func() { _ = rc.Close() }()

		fileData, err := io.ReadAll(rc)
		if err != nil {
			return err
		}

		ext := path.Ext(name)
		base := strings.TrimSuffix(name, ext)

		var manifestElement *models.UploadElement
		if strings.HasPrefix(strings.ToLower(name), "package") && strings.ToLower(ext) == ".swift" {
			manifestElement = models.NewUploadElement(element.Scope, element.Name, element.Version, mimetypes.TextXSwift, models.Manifest)
			manifestElement.SetFilenameOverwrite(base)
		} else if strings.ToLower(name) == "package.json" {
			manifestElement = models.NewUploadElement(element.Scope, element.Name, element.Version, mimetypes.ApplicationJson, models.PackageManifestJson)
		} else {
			return nil
		}

		writer, err := r.GetWriter(ctx, manifestElement)
		if err != nil {
			slog.Warn("Failed to get writer for manifest", "manifest", name, "error", err)
			return nil
		}

		if _, err := writer.Write(fileData); err != nil {
			_ = writer.Close()
			slog.Warn("Failed to write manifest", "manifest", name, "error", err)
			return nil
		}

		if err := writer.Close(); err != nil {
			slog.Warn("Failed to upload manifest", "manifest", name, "error", err)
		}

		return nil
	}

	return files.ExtractManifestFilesFromZipReader(element, zipReader, fileExtractor)
}

// List returns all versions of a package.
func (r *S3Repo) List(ctx context.Context, scope string, name string) ([]models.ListElement, error) {
	prefix := buildPrefix(r.config, scope, name)
	commonPrefixes, err := r.listCommonPrefixes(ctx, prefix)
	if err != nil {
		return nil, err
	}

	elements := make([]models.ListElement, 0, len(commonPrefixes))
	for _, cp := range commonPrefixes {
		version := lastSegment(cp)
		elements = append(elements, *models.NewListElement(scope, name, version))
	}

	return elements, nil
}

// EncodeBase64 returns the base64 representation of the content of the provided element.
func (r *S3Repo) EncodeBase64(ctx context.Context, element *models.UploadElement) (string, error) {
	if !r.Exists(ctx, element) {
		return "", fmt.Errorf("file not exists: %s", element.FileName())
	}

	reader, err := r.GetReader(ctx, element)
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(data), nil
}

// PublishDate returns the date the element was uploaded, from S3's LastModified.
func (r *S3Repo) PublishDate(ctx context.Context, element *models.UploadElement) (time.Time, error) {
	key := buildElementKey(r.config, element)

	out, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return r.timeProvider.Now(), fmt.Errorf("object does not exist: %s", key)
	}

	if out.LastModified == nil {
		return r.timeProvider.Now(), nil
	}

	return *out.LastModified, nil
}

// LoadMetadata retrieves the metadata of the package.
func (r *S3Repo) LoadMetadata(ctx context.Context, scope string, name string, version string) (map[string]any, error) {
	element := models.NewUploadElement(scope, name, version, mimetypes.ApplicationJson, models.Metadata)
	if !r.Exists(ctx, element) {
		return nil, fmt.Errorf("file not exists: %s", element.FileName())
	}

	reader, err := r.GetReader(ctx, element)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	return metadata, nil
}

// Checksum provides the sha256 checksum of the element.
// It first tries the sha256 user-metadata stored at upload time, falling back to
// downloading and hashing the object if that metadata is missing (e.g. objects uploaded
// out-of-band).
func (r *S3Repo) Checksum(ctx context.Context, element *models.UploadElement) (string, error) {
	key := buildElementKey(r.config, element)

	out, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("file not exists: %s", element.FileName())
	}

	for metaKey, value := range out.Metadata {
		if strings.EqualFold(metaKey, checksumMetadataKey) && len(value) == sha256.Size*2 {
			return value, nil
		}
	}

	reader, err := r.getReaderForKey(ctx, key)
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// GetAlternativeManifests returns the alternative versions of the manifest.
func (r *S3Repo) GetAlternativeManifests(ctx context.Context, element *models.UploadElement) ([]models.UploadElement, error) {
	versionPrefix := buildPrefix(r.config, element.Scope, element.Name, element.Version)

	keys, err := r.listKeysWithSuffix(ctx, versionPrefix, "")
	if err != nil {
		return nil, err
	}

	var manifests []models.UploadElement
	for _, key := range keys {
		filename := lastSegment(key)
		ext := path.Ext(filename)
		if filename != "Package.swift" && strings.HasPrefix(filename, "Package") && strings.ToLower(ext) == ".swift" {
			manifest := models.NewUploadElement(element.Scope, element.Name, element.Version, mimetypes.TextXSwift, models.Manifest)
			manifest.SetFilenameOverwrite(strings.TrimSuffix(filename, ext))
			manifests = append(manifests, *manifest)
		}
	}

	return manifests, nil
}

// GetSwiftToolVersion returns the swift tool version specified in the first line of the manifest file.
func (r *S3Repo) GetSwiftToolVersion(ctx context.Context, manifest *models.UploadElement) (string, error) {
	if !r.Exists(ctx, manifest) {
		return "", fmt.Errorf("file not exists: %s", manifest.FileName())
	}

	reader, err := r.GetReader(ctx, manifest)
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	const swiftVersionPrefix = "// swift-tools-version:"
	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, swiftVersionPrefix); ok {
			return strings.TrimSpace(after), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("swift-tools-version not found")
}

// Lookup returns the list of identifiers for the provided repository url.
func (r *S3Repo) Lookup(ctx context.Context, url string) []string {
	var result []string

	prefix := buildPrefix(r.config)
	keys, err := r.listKeysWithSuffix(ctx, prefix, "metadata.json")
	if err != nil {
		slog.Error("Error listing metadata objects:", "error", err)
		return result
	}

	for _, key := range keys {
		scope, name, version, ok := r.parseScopeNameVersion(key)
		if !ok {
			continue
		}

		metadata, err := r.LoadMetadata(ctx, scope, name, version)
		if err != nil {
			continue
		}

		if repositoryURLs, ok := metadata["repositoryURLs"].([]any); ok {
			for _, repoURL := range repositoryURLs {
				if repoURLStr, ok := repoURL.(string); ok && repoURLStr == url {
					result = append(result, fmt.Sprintf("%s.%s", scope, name))
					return result
				}
			}
		}
	}

	return result
}

// Remove deletes the provided element.
func (r *S3Repo) Remove(ctx context.Context, element *models.UploadElement) error {
	key := buildElementKey(r.config, element)
	if !r.Exists(ctx, element) {
		return fmt.Errorf("file not exists: %s", element.FileName())
	}

	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.config.Bucket),
		Key:    aws.String(key),
	})
	return err
}

// ListScopes returns all available scopes in the registry.
func (r *S3Repo) ListScopes(ctx context.Context) ([]string, error) {
	commonPrefixes, err := r.listCommonPrefixes(ctx, buildPrefix(r.config))
	if err != nil {
		return nil, err
	}

	scopes := make([]string, 0, len(commonPrefixes))
	for _, cp := range commonPrefixes {
		scopes = append(scopes, lastSegment(cp))
	}

	return scopes, nil
}

// ListInScope returns all packages in a specific scope.
func (r *S3Repo) ListInScope(ctx context.Context, scope string) ([]models.ListElement, error) {
	commonPrefixes, err := r.listCommonPrefixes(ctx, buildPrefix(r.config, scope))
	if err != nil {
		return nil, err
	}

	var packages []models.ListElement
	for _, cp := range commonPrefixes {
		name := lastSegment(cp)
		versions, err := r.List(ctx, scope, name)
		if err == nil {
			packages = append(packages, versions...)
		}
	}

	return packages, nil
}

// ListAll returns all packages across all scopes.
func (r *S3Repo) ListAll(ctx context.Context) ([]models.ListElement, error) {
	scopes, err := r.ListScopes(ctx)
	if err != nil {
		return nil, err
	}

	var allPackages []models.ListElement
	for _, scope := range scopes {
		packages, err := r.ListInScope(ctx, scope)
		if err != nil {
			slog.Warn("Error listing packages in scope", "scope", scope, "error", err)
			continue
		}
		allPackages = append(allPackages, packages...)
	}

	return allPackages, nil
}

// LoadPackageJson loads the Package.json file for a package version.
func (r *S3Repo) LoadPackageJson(ctx context.Context, scope string, name string, version string) (map[string]any, error) {
	element := models.NewUploadElement(scope, name, version, mimetypes.ApplicationJson, models.PackageManifestJson)
	if !r.Exists(ctx, element) {
		return nil, fmt.Errorf("package.json not found for %s.%s@%s", scope, name, version)
	}

	reader, err := r.GetReader(ctx, element)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	var packageJson map[string]any
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&packageJson); err != nil {
		return nil, fmt.Errorf("failed to parse Package.json: %w", err)
	}

	return packageJson, nil
}

// parseScopeNameVersion splits an object key of the form <prefix>/<scope>/<name>/<version>/<file>
// into its scope, name and version components.
func (r *S3Repo) parseScopeNameVersion(key string) (scope string, name string, version string, ok bool) {
	rel := key
	if prefixKey := buildKey(r.config); prefixKey != "" {
		rel = strings.TrimPrefix(key, prefixKey+"/")
	}

	parts := strings.Split(rel, "/")
	if len(parts) < 4 {
		return "", "", "", false
	}

	return parts[0], parts[1], parts[2], true
}

// getReaderForKey exposes access.getReaderForKey for use within S3Repo (e.g. Checksum fallback).
func (r *S3Repo) getReaderForKey(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	return r.Access.(*access).getReaderForKey(ctx, key)
}
