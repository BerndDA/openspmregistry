package s3

import (
	"OpenSPMRegistry/config"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// checksumMetadataKey is the S3 user-metadata key (x-amz-meta-sha256) used to store the
// sha256 checksum of an object at upload time, so Checksum() can avoid a full re-download.
const checksumMetadataKey = "sha256"

// s3Writer implements io.WriteCloser and uploads the buffered data via PutObject on Close().
type s3Writer struct {
	ctx         context.Context
	client      s3API
	config      config.S3Config
	key         string
	contentType string
	buffer      bytes.Buffer
}

func newS3Writer(ctx context.Context, client s3API, cfg config.S3Config, key string, contentType string) *s3Writer {
	return &s3Writer{
		ctx:         ctx,
		client:      client,
		config:      cfg,
		key:         key,
		contentType: contentType,
	}
}

func (w *s3Writer) Write(p []byte) (int, error) {
	return w.buffer.Write(p)
}

func (w *s3Writer) Close() error {
	data := w.buffer.Bytes()
	checksum := sha256.Sum256(data)

	_, err := w.client.PutObject(w.ctx, &s3.PutObjectInput{
		Bucket:      aws.String(w.config.Bucket),
		Key:         aws.String(w.key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(w.contentType),
		Metadata:    map[string]string{checksumMetadataKey: hex.EncodeToString(checksum[:])},
	})
	if err != nil {
		return fmt.Errorf("failed to upload object %s: %w", w.key, err)
	}
	return nil
}
