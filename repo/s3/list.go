package s3

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const listDelimiter = "/"

// listCommonPrefixes lists the immediate "directory" entries under prefix (delimited by "/"),
// e.g. for prefix "scope/" it returns "scope/name1/", "scope/name2/", ... one level deep.
func (r *S3Repo) listCommonPrefixes(ctx context.Context, prefix string) ([]string, error) {
	var result []string
	var continuationToken *string

	for {
		out, err := r.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(r.config.Bucket),
			Prefix:            aws.String(prefix),
			Delimiter:         aws.String(listDelimiter),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, err
		}

		for _, cp := range out.CommonPrefixes {
			if cp.Prefix != nil {
				result = append(result, *cp.Prefix)
			}
		}

		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		continuationToken = out.NextContinuationToken
	}

	return result, nil
}

// listKeysWithSuffix lists all keys under prefix (recursively, no delimiter) whose name ends with suffix.
func (r *S3Repo) listKeysWithSuffix(ctx context.Context, prefix string, suffix string) ([]string, error) {
	var result []string
	var continuationToken *string

	for {
		out, err := r.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(r.config.Bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, err
		}

		for _, obj := range out.Contents {
			if obj.Key != nil && strings.HasSuffix(*obj.Key, suffix) {
				result = append(result, *obj.Key)
			}
		}

		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		continuationToken = out.NextContinuationToken
	}

	return result, nil
}

// lastSegment returns the final "/"-delimited, non-empty segment of a key/prefix.
func lastSegment(key string) string {
	trimmed := strings.TrimSuffix(key, listDelimiter)
	idx := strings.LastIndex(trimmed, listDelimiter)
	if idx < 0 {
		return trimmed
	}
	return trimmed[idx+1:]
}
