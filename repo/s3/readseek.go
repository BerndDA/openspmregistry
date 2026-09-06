package s3

import "bytes"

// bufferedReadSeekCloser implements io.ReadSeekCloser by buffering an S3 object's
// full content in memory. Packages served by this registry are small (bounded by
// publish.maxSize), so buffering is simpler and cheap compared to range requests.
type bufferedReadSeekCloser struct {
	*bytes.Reader
}

func (b *bufferedReadSeekCloser) Close() error {
	return nil
}
