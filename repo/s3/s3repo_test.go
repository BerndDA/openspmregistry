package s3

import (
	"OpenSPMRegistry/config"
	"OpenSPMRegistry/mimetypes"
	"OpenSPMRegistry/models"
	"OpenSPMRegistry/utils"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func newTestRepo(client s3API, cfg config.S3Config) *S3Repo {
	return &S3Repo{
		Access:       newAccess(client, cfg),
		client:       client,
		config:       cfg,
		timeProvider: utils.NewRealTimeProvider(),
	}
}

func Test_List_NoVersions_ReturnsEmptyList(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	elements, err := repo.List(context.Background(), "scope", "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(elements) != 0 {
		t.Errorf("expected empty list, got %v", elements)
	}
}

func Test_List_MultipleVersions_ReturnsAll(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket", Prefix: "registry"}
	repo := newTestRepo(client, cfg)

	for _, v := range []string{"1.0.0", "2.0.0"} {
		client.put(buildKey(cfg, "scope", "name", v, "name-"+v+".zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())
	}

	elements, err := repo.List(context.Background(), "scope", "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(elements))
	}
	found := map[string]bool{}
	for _, e := range elements {
		found[e.Version] = true
		if e.Scope != "scope" || e.PackageName != "name" {
			t.Errorf("unexpected element %+v", e)
		}
	}
	if !found["1.0.0"] || !found["2.0.0"] {
		t.Errorf("expected versions 1.0.0 and 2.0.0, got %v", found)
	}
}

func Test_ListScopes_MultipleScopes_ReturnsAll(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	client.put(buildKey(cfg, "scopeA", "name", "1.0.0", "file.zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())
	client.put(buildKey(cfg, "scopeB", "name", "1.0.0", "file.zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())

	scopes, err := repo.ListScopes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %v", scopes)
	}
}

func Test_ListInScope_MultiplePackages_ReturnsVersionsForEach(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	client.put(buildKey(cfg, "scope", "pkgA", "1.0.0", "file.zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())
	client.put(buildKey(cfg, "scope", "pkgB", "2.0.0", "file.zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())

	packages, err := repo.ListInScope(context.Background(), "scope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 2 {
		t.Fatalf("expected 2 packages, got %v", packages)
	}
}

func Test_ListAll_CombinesScopesAndPackages(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	client.put(buildKey(cfg, "scopeA", "pkgA", "1.0.0", "file.zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())
	client.put(buildKey(cfg, "scopeB", "pkgB", "1.0.0", "file.zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())

	all, err := repo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 packages, got %v", all)
	}
}

func Test_EncodeBase64_FileExists_ReturnsBase64(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	client.put(buildElementKey(cfg, element), []byte("hi"), mimetypes.ApplicationZip, nil, time.Now())

	got, err := repo.EncodeBase64(context.Background(), element)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "aGk="
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func Test_EncodeBase64_FileDoesNotExist_ReturnsError(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	if _, err := repo.EncodeBase64(context.Background(), element); err == nil {
		t.Error("expected error for missing file")
	}
}

func Test_PublishDate_ValidFile_ReturnsLastModified(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	expected := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	client.put(buildElementKey(cfg, element), []byte("data"), mimetypes.ApplicationZip, nil, expected)

	got, err := repo.PublishDate(context.Background(), element)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

func Test_PublishDate_FileDoesNotExist_ReturnsError(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	if _, err := repo.PublishDate(context.Background(), element); err == nil {
		t.Error("expected error for missing file")
	}
}

func Test_LoadMetadata_ValidFile_ReturnsMetadata(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationJson, models.Metadata)
	body, _ := json.Marshal(map[string]any{"description": "a package"})
	client.put(buildElementKey(cfg, element), body, mimetypes.ApplicationJson, nil, time.Now())

	metadata, err := repo.LoadMetadata(context.Background(), "scope", "name", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metadata["description"] != "a package" {
		t.Errorf("unexpected metadata: %v", metadata)
	}
}

func Test_LoadMetadata_FileDoesNotExist_ReturnsError(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	if _, err := repo.LoadMetadata(context.Background(), "scope", "name", "1.0.0"); err == nil {
		t.Error("expected error for missing metadata")
	}
}

func Test_LoadPackageJson_ValidFile_ReturnsJson(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationJson, models.PackageManifestJson)
	body, _ := json.Marshal(map[string]any{"name": "MyPackage"})
	client.put(buildElementKey(cfg, element), body, mimetypes.ApplicationJson, nil, time.Now())

	pkgJson, err := repo.LoadPackageJson(context.Background(), "scope", "name", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pkgJson["name"] != "MyPackage" {
		t.Errorf("unexpected package json: %v", pkgJson)
	}
}

func Test_Checksum_MetadataPresent_ReturnsStoredChecksum(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	storedChecksum := "abc123"
	// pad to 64 chars to satisfy the length check
	for len(storedChecksum) < 64 {
		storedChecksum += "0"
	}
	client.put(buildElementKey(cfg, element), []byte("data"), mimetypes.ApplicationZip, map[string]string{"sha256": storedChecksum}, time.Now())

	got, err := repo.Checksum(context.Background(), element)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != storedChecksum {
		t.Errorf("expected %q, got %q", storedChecksum, got)
	}
}

func Test_Checksum_MetadataMissing_ComputesFromContent(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	client.put(buildElementKey(cfg, element), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())

	got, err := repo.Checksum(context.Background(), element)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// sha256("data")
	want := "3a6eb0790f39ac87c94f3856b2dd2c5d110e6811602261a9a923d3bb23adc8b7"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func Test_Checksum_FileDoesNotExist_ReturnsError(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	if _, err := repo.Checksum(context.Background(), element); err == nil {
		t.Error("expected error for missing file")
	}
}

func Test_Remove_FileExists_DeletesObject(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	client.put(buildElementKey(cfg, element), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())

	if err := repo.Remove(context.Background(), element); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.Exists(context.Background(), element) {
		t.Error("expected element to be removed")
	}
}

func Test_Remove_FileDoesNotExist_ReturnsError(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	if err := repo.Remove(context.Background(), element); err == nil {
		t.Error("expected error for missing file")
	}
}

func Test_GetSwiftToolVersion_ValidManifest_ReturnsVersion(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	manifest := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.TextXSwift, models.Manifest)
	client.put(buildElementKey(cfg, manifest), []byte("// swift-tools-version:5.9\nimport PackageDescription\n"), mimetypes.TextXSwift, nil, time.Now())

	got, err := repo.GetSwiftToolVersion(context.Background(), manifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "5.9" {
		t.Errorf("expected '5.9', got %q", got)
	}
}

func Test_GetAlternativeManifests_MultipleManifests_ReturnsNonDefault(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	versionPrefix := buildPrefix(cfg, "scope", "name", "1.0.0")
	client.put(versionPrefix+"Package.swift", []byte("data"), mimetypes.TextXSwift, nil, time.Now())
	client.put(versionPrefix+"Package@swift-5.8.swift", []byte("data"), mimetypes.TextXSwift, nil, time.Now())
	client.put(versionPrefix+"Package.json", []byte("{}"), mimetypes.ApplicationJson, nil, time.Now())

	manifests, err := repo.GetAlternativeManifests(context.Background(), element)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("expected 1 alternative manifest, got %d: %v", len(manifests), manifests)
	}
	if manifests[0].FileName() != "Package@swift-5.8.swift" {
		t.Errorf("unexpected manifest filename: %s", manifests[0].FileName())
	}
}

func Test_Lookup_MatchingRepositoryURL_ReturnsIdentifier(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	metadataElement := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationJson, models.Metadata)
	body, _ := json.Marshal(map[string]any{"repositoryURLs": []string{"https://example.com/scope/name.git"}})
	client.put(buildElementKey(cfg, metadataElement), body, mimetypes.ApplicationJson, nil, time.Now())

	result := repo.Lookup(context.Background(), "https://example.com/scope/name.git")
	if len(result) != 1 || result[0] != "scope.name" {
		t.Errorf("expected [scope.name], got %v", result)
	}
}

func Test_Lookup_NoMatch_ReturnsEmpty(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	result := repo.Lookup(context.Background(), "https://example.com/none.git")
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func Test_ExtractManifestFiles_ValidZip_ExtractsManifestAndPackageJson(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w1, _ := zw.Create("scope.name/Package.swift")
	_, _ = w1.Write([]byte("// swift-tools-version:5.9\n"))
	w2, _ := zw.Create("scope.name/Package.json")
	_, _ = w2.Write([]byte(`{"name":"name"}`))
	_ = zw.Close()

	sourceElement := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	client.put(buildElementKey(cfg, sourceElement), buf.Bytes(), mimetypes.ApplicationZip, nil, time.Now())

	if err := repo.ExtractManifestFiles(context.Background(), sourceElement); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manifestElement := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.TextXSwift, models.Manifest)
	if !repo.Exists(context.Background(), manifestElement) {
		t.Error("expected Package.swift to be extracted")
	}

	packageJsonElement := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationJson, models.PackageManifestJson)
	if !repo.Exists(context.Background(), packageJsonElement) {
		t.Error("expected Package.json to be extracted")
	}
}

func Test_ExtractManifestFiles_UnsupportedMimeType_ReturnsError(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.TextXSwift, models.Manifest)
	if err := repo.ExtractManifestFiles(context.Background(), element); err == nil {
		t.Error("expected error for unsupported mime type")
	}
}

func Test_listCommonPrefixes_Paginated_ReturnsAllPages(t *testing.T) {
	client := newMockS3Client()
	client.pageSize = 1
	cfg := config.S3Config{Bucket: "test-bucket"}
	repo := newTestRepo(client, cfg)

	client.put(buildKey(cfg, "scopeA", "pkg", "1.0.0", "file.zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())
	client.put(buildKey(cfg, "scopeB", "pkg", "1.0.0", "file.zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())
	client.put(buildKey(cfg, "scopeC", "pkg", "1.0.0", "file.zip"), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())

	scopes, err := repo.ListScopes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scopes) != 3 {
		t.Fatalf("expected 3 scopes across pages, got %v", scopes)
	}
}
