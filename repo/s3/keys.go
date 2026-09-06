package s3

import (
	"OpenSPMRegistry/config"
	"OpenSPMRegistry/models"
	"path"
	"strings"
)

// buildKey joins the configured prefix with the given path segments into an S3 object key.
// S3 keys always use "/" regardless of host OS, so path.Join (not filepath.Join) is used.
func buildKey(cfg config.S3Config, parts ...string) string {
	all := append([]string{cfg.Prefix}, parts...)
	return strings.TrimPrefix(path.Join(all...), "/")
}

// buildPrefix is like buildKey but guarantees a trailing "/" (when non-empty),
// suitable for use as a ListObjectsV2 Prefix that should only match "directory" contents.
func buildPrefix(cfg config.S3Config, parts ...string) string {
	key := buildKey(cfg, parts...)
	if key == "" {
		return ""
	}
	return key + "/"
}

// buildElementKey builds the object key for an UploadElement.
func buildElementKey(cfg config.S3Config, element *models.UploadElement) string {
	return buildKey(cfg, element.Scope, element.Name, element.Version, element.FileName())
}
