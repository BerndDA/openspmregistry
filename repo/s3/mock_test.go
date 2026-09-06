package s3

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// mockObject is an in-memory stand-in for an S3 object.
type mockObject struct {
	data         []byte
	contentType  string
	metadata     map[string]string
	lastModified time.Time
}

// mockS3Client is a minimal in-memory fake of the s3API interface, used so unit tests can
// exercise S3Repo/access logic without talking to AWS.
type mockS3Client struct {
	objects map[string]*mockObject
	// pageSize, when > 0, caps how many keys ListObjectsV2 returns per call to exercise pagination.
	pageSize int
}

func newMockS3Client() *mockS3Client {
	return &mockS3Client{objects: make(map[string]*mockObject)}
}

func (m *mockS3Client) put(key string, data []byte, contentType string, metadata map[string]string, lastModified time.Time) {
	m.objects[key] = &mockObject{data: data, contentType: contentType, metadata: metadata, lastModified: lastModified}
}

func (m *mockS3Client) HeadObject(_ context.Context, params *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	obj, ok := m.objects[aws.ToString(params.Key)]
	if !ok {
		return nil, &types.NotFound{}
	}
	lastModified := obj.lastModified
	return &s3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(obj.data))),
		LastModified:  &lastModified,
		Metadata:      obj.metadata,
	}, nil
}

func (m *mockS3Client) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	obj, ok := m.objects[aws.ToString(params.Key)]
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(obj.data)),
		ContentLength: aws.Int64(int64(len(obj.data))),
	}, nil
}

func (m *mockS3Client) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	data, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	m.objects[aws.ToString(params.Key)] = &mockObject{
		data:         data,
		contentType:  aws.ToString(params.ContentType),
		metadata:     params.Metadata,
		lastModified: time.Now(),
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(m.objects, aws.ToString(params.Key))
	return &s3.DeleteObjectOutput{}, nil
}

// ListObjectsV2 is a simplified emulation of the real API: it supports Prefix and Delimiter
// (producing CommonPrefixes the same way S3 does) and pagination via ContinuationToken, where
// the token is simply the key to resume listing from (an implementation detail private to this mock).
func (m *mockS3Client) ListObjectsV2(_ context.Context, params *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(params.Prefix)
	delimiter := aws.ToString(params.Delimiter)

	var keys []string
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	if params.ContinuationToken != nil {
		token := aws.ToString(params.ContinuationToken)
		idx := sort.SearchStrings(keys, token)
		keys = keys[idx:]
	}

	pageSize := m.pageSize
	truncated := false
	var nextToken *string
	if pageSize > 0 && len(keys) > pageSize {
		nextToken = aws.String(keys[pageSize])
		keys = keys[:pageSize]
		truncated = true
	}

	var contents []types.Object
	var commonPrefixes []types.CommonPrefix
	seenPrefixes := make(map[string]bool)

	for _, k := range keys {
		rest := strings.TrimPrefix(k, prefix)
		if delimiter != "" {
			if idx := strings.Index(rest, delimiter); idx >= 0 {
				cp := prefix + rest[:idx+len(delimiter)]
				if !seenPrefixes[cp] {
					seenPrefixes[cp] = true
					commonPrefixes = append(commonPrefixes, types.CommonPrefix{Prefix: aws.String(cp)})
				}
				continue
			}
		}
		obj := m.objects[k]
		lastModified := obj.lastModified
		contents = append(contents, types.Object{Key: aws.String(k), LastModified: &lastModified})
	}

	return &s3.ListObjectsV2Output{
		CommonPrefixes:        commonPrefixes,
		Contents:              contents,
		IsTruncated:           aws.Bool(truncated),
		NextContinuationToken: nextToken,
	}, nil
}
