package s3

import (
	"OpenSPMRegistry/config"
	"OpenSPMRegistry/models"
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type access struct {
	client s3API
	config config.S3Config
}

func newAccess(client s3API, cfg config.S3Config) *access {
	return &access{client: client, config: cfg}
}

// Exists checks whether the object exists via HeadObject.
func (a *access) Exists(ctx context.Context, element *models.UploadElement) bool {
	return a.existsKey(ctx, buildElementKey(a.config, element))
}

func (a *access) existsKey(ctx context.Context, key string) bool {
	_, err := a.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(a.config.Bucket),
		Key:    aws.String(key),
	})
	return err == nil
}

// GetReader returns a buffered reader for the specified element.
func (a *access) GetReader(ctx context.Context, element *models.UploadElement) (io.ReadSeekCloser, error) {
	return a.getReaderForKey(ctx, buildElementKey(a.config, element))
}

func (a *access) getReaderForKey(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	out, err := a.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read object %s: %w", key, err)
	}

	return &bufferedReadSeekCloser{Reader: bytes.NewReader(data)}, nil
}

// GetWriter returns a writer that uploads the element via PutObject on Close().
func (a *access) GetWriter(ctx context.Context, element *models.UploadElement) (io.WriteCloser, error) {
	key := buildElementKey(a.config, element)
	return newS3Writer(ctx, a.client, a.config, key, element.MimeType), nil
}
