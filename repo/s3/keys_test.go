package s3

import (
	"OpenSPMRegistry/config"
	"OpenSPMRegistry/mimetypes"
	"OpenSPMRegistry/models"
	"testing"
)

func Test_buildKey_NoPrefix_JoinsParts(t *testing.T) {
	cfg := config.S3Config{}
	got := buildKey(cfg, "scope", "name", "1.0.0")
	want := "scope/name/1.0.0"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func Test_buildKey_WithPrefix_JoinsPrefixAndParts(t *testing.T) {
	cfg := config.S3Config{Prefix: "registry"}
	got := buildKey(cfg, "scope", "name", "1.0.0")
	want := "registry/scope/name/1.0.0"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func Test_buildKey_PrefixWithSlashes_IsNormalized(t *testing.T) {
	cfg := config.S3Config{Prefix: "/registry/"}
	got := buildKey(cfg, "scope")
	want := "registry/scope"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func Test_buildPrefix_NoParts_EmptyPrefix_ReturnsEmpty(t *testing.T) {
	cfg := config.S3Config{}
	got := buildPrefix(cfg)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func Test_buildPrefix_WithParts_HasTrailingSlash(t *testing.T) {
	cfg := config.S3Config{Prefix: "registry"}
	got := buildPrefix(cfg, "scope", "name")
	want := "registry/scope/name/"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func Test_buildElementKey_SourceArchive_ReturnsCorrectPath(t *testing.T) {
	cfg := config.S3Config{}
	element := models.NewUploadElement("testScope", "my-package", "1.0.0", mimetypes.ApplicationZip, models.SourceArchive)
	got := buildElementKey(cfg, element)
	want := "testScope/my-package/1.0.0/" + element.FileName()
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func Test_lastSegment_TrailingSlash_ReturnsLastNonEmptySegment(t *testing.T) {
	got := lastSegment("registry/scope/name/")
	want := "name"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func Test_lastSegment_NoSlash_ReturnsInput(t *testing.T) {
	got := lastSegment("scope")
	want := "scope"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
