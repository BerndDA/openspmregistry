package s3

import (
	"OpenSPMRegistry/config"
	"OpenSPMRegistry/mimetypes"
	"OpenSPMRegistry/models"
	"context"
	"io"
	"testing"
	"time"
)

func Test_access_Exists_ObjectPresent_ReturnsTrue(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	client.put(buildElementKey(cfg, element), []byte("data"), mimetypes.ApplicationZip, nil, time.Now())

	a := newAccess(client, cfg)
	if !a.Exists(context.Background(), element) {
		t.Error("expected element to exist")
	}
}

func Test_access_Exists_ObjectMissing_ReturnsFalse(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)

	a := newAccess(client, cfg)
	if a.Exists(context.Background(), element) {
		t.Error("expected element to not exist")
	}
}

func Test_access_GetReader_ObjectPresent_ReturnsContent(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	client.put(buildElementKey(cfg, element), []byte("hello world"), mimetypes.ApplicationZip, nil, time.Now())

	a := newAccess(client, cfg)
	reader, err := a.GetReader(context.Background(), element)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

func Test_access_GetReader_Seek_ReturnsContentFromOffset(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	client.put(buildElementKey(cfg, element), []byte("hello world"), mimetypes.ApplicationZip, nil, time.Now())

	a := newAccess(client, cfg)
	reader, err := a.GetReader(context.Background(), element)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = reader.Close() }()

	if _, err := reader.Seek(6, io.SeekStart); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world', got %q", string(data))
	}
}

func Test_access_GetReader_ObjectMissing_ReturnsError(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)

	a := newAccess(client, cfg)
	if _, err := a.GetReader(context.Background(), element); err == nil {
		t.Error("expected error for missing object")
	}
}

func Test_access_GetWriter_WriteAndClose_UploadsObject(t *testing.T) {
	client := newMockS3Client()
	cfg := config.S3Config{Bucket: "test-bucket"}
	element := models.NewUploadElement("scope", "name", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)

	a := newAccess(client, cfg)
	writer, err := a.GetWriter(context.Background(), element)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := writer.Write([]byte("uploaded data")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !a.Exists(context.Background(), element) {
		t.Error("expected element to exist after upload")
	}

	obj, ok := client.objects[buildElementKey(cfg, element)]
	if !ok {
		t.Fatal("expected object to be stored")
	}
	if string(obj.data) != "uploaded data" {
		t.Errorf("expected 'uploaded data', got %q", string(obj.data))
	}
	if obj.metadata[checksumMetadataKey] == "" {
		t.Error("expected sha256 checksum metadata to be set")
	}
}
