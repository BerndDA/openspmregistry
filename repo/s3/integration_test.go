//go:build integration
// +build integration

package s3

import (
	"OpenSPMRegistry/config"
	"OpenSPMRegistry/mimetypes"
	"OpenSPMRegistry/models"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// getIntegrationConfig returns an S3Config for integration tests against a real bucket.
// Defaults match the "spm-data" bucket provisioned via spm_registry/terraform on the
// "aws-spielwiese" AWS profile; override via env vars to point at a different bucket/profile.
func getIntegrationConfig(t *testing.T) config.S3Config {
	bucket := os.Getenv("S3_TEST_BUCKET")
	if bucket == "" {
		bucket = "spm-data-286123791725-eu-central-1-an"
	}
	region := os.Getenv("S3_TEST_REGION")
	if region == "" {
		region = "eu-central-1"
	}
	profile := os.Getenv("S3_TEST_PROFILE")
	if profile == "" {
		profile = "aws-spielwiese"
	}

	prefixBytes := make([]byte, 8)
	if _, err := rand.Read(prefixBytes); err != nil {
		t.Fatalf("failed to generate random prefix: %v", err)
	}

	return config.S3Config{
		Bucket:  bucket,
		Region:  region,
		Profile: profile,
		Prefix:  "integration-test/" + hex.EncodeToString(prefixBytes),
	}
}

// cleanup removes every object under the test prefix so repeated runs don't accumulate garbage.
func cleanup(t *testing.T, ctx context.Context, repo *S3Repo, cfg config.S3Config) {
	t.Helper()
	keys, err := repo.listKeysWithSuffix(ctx, buildPrefix(cfg), "")
	if err != nil {
		t.Logf("cleanup: failed to list objects: %v", err)
		return
	}
	for _, key := range keys {
		input := &s3.DeleteObjectInput{Bucket: aws.String(cfg.Bucket), Key: aws.String(key)}
		if _, err := repo.client.DeleteObject(ctx, input); err != nil {
			t.Logf("cleanup: failed to delete %s: %v", key, err)
		}
	}
}

func TestIntegration_PublishListReadRemove_RealBucket(t *testing.T) {
	ctx := context.Background()
	cfg := getIntegrationConfig(t)

	repo, err := NewS3Repo(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create S3 repo (check AWS SSO login for profile %q): %v", cfg.Profile, err)
	}
	defer cleanup(t, ctx, repo, cfg)

	scope := "integrationscope"
	name := "IntegrationPackage"
	version := "1.0.0"

	sourceArchive := models.NewUploadElement(scope, name, version, mimetypes.ApplicationZip, models.SourceArchive)
	content := []byte("integration test payload")

	writer, err := repo.GetWriter(ctx, sourceArchive)
	if err != nil {
		t.Fatalf("GetWriter failed: %v", err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close (upload) failed: %v", err)
	}

	if !repo.Exists(ctx, sourceArchive) {
		t.Fatal("expected uploaded source archive to exist")
	}

	reader, err := repo.GetReader(ctx, sourceArchive)
	if err != nil {
		t.Fatalf("GetReader failed: %v", err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("expected %q, got %q", content, data)
	}

	if _, err := repo.PublishDate(ctx, sourceArchive); err != nil {
		t.Errorf("PublishDate failed: %v", err)
	}

	checksum, err := repo.Checksum(ctx, sourceArchive)
	if err != nil {
		t.Errorf("Checksum failed: %v", err)
	}
	if len(checksum) != 64 {
		t.Errorf("expected a 64-char sha256 checksum, got %q", checksum)
	}

	metadataElement := models.NewUploadElement(scope, name, version, mimetypes.ApplicationJson, models.Metadata)
	metadataBody, _ := json.Marshal(map[string]any{"description": "integration test package"})
	metaWriter, err := repo.GetWriter(ctx, metadataElement)
	if err != nil {
		t.Fatalf("GetWriter (metadata) failed: %v", err)
	}
	if _, err := metaWriter.Write(metadataBody); err != nil {
		t.Fatalf("Write (metadata) failed: %v", err)
	}
	if err := metaWriter.Close(); err != nil {
		t.Fatalf("Close (metadata upload) failed: %v", err)
	}

	metadata, err := repo.LoadMetadata(ctx, scope, name, version)
	if err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}
	if metadata["description"] != "integration test package" {
		t.Errorf("unexpected metadata: %v", metadata)
	}

	versions, err := repo.List(ctx, scope, name)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(versions) != 1 || versions[0].Version != version {
		t.Errorf("expected [%s], got %v", version, versions)
	}

	scopes, err := repo.ListScopes(ctx)
	if err != nil {
		t.Fatalf("ListScopes failed: %v", err)
	}
	found := false
	for _, s := range scopes {
		if s == scope {
			found = true
		}
	}
	if !found {
		t.Errorf("expected scope %q in %v", scope, scopes)
	}

	if err := repo.Remove(ctx, sourceArchive); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if repo.Exists(ctx, sourceArchive) {
		t.Error("expected source archive to be removed")
	}
}
