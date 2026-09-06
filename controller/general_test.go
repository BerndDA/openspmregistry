package controller

import (
	"OpenSPMRegistry/config"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errorWriter is a writer that always fails
type errorWriter struct {
	http.ResponseWriter
}

func Test_MainAction_Returns404(t *testing.T) {
	// Create controller with minimal config
	c := NewController(config.ServerConfig{}, nil)

	// Create test request and response recorder
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	// Call MainAction
	c.MainAction(w, req)

	// Check status code
	if w.Code != http.StatusNotFound {
		t.Errorf("expected status code %d, got %d", http.StatusNotFound, w.Code)
	}

	// Check response headers
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("expected Content-Type %s, got %s", "application/problem+json", contentType)
	}

	contentLanguage := w.Header().Get("Content-Language")
	if contentLanguage != "en" {
		t.Errorf("expected Content-Language %s, got %s", "en", contentLanguage)
	}

	// Check response body
	var response struct {
		Detail string `json:"detail"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if response.Detail != "Not found" {
		t.Errorf("expected error detail %q, got %q", "Not found", response.Detail)
	}
}

func Test_StaticAction_ServesFiles(t *testing.T) {
	// Create a temporary directory for static files
	tmpDir, err := os.MkdirTemp("", "static")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a test static file
	testContent := "test static content"
	testFilePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create controller with minimal config
	c := NewController(config.ServerConfig{}, nil)

	// Create test request and response recorder
	req := httptest.NewRequest("GET", "/test.txt", nil)
	w := httptest.NewRecorder()

	// Create symbolic link from static directory to temp directory
	if err := os.MkdirAll("static", 0755); err != nil {
		t.Fatalf("failed to create static directory: %v", err)
	}
	defer func() { _ = os.RemoveAll("static") }()

	if err := os.Symlink(testFilePath, "static/test.txt"); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Call StaticAction
	c.StaticAction(w, req)

	// Check status code
	if w.Code != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, w.Code)
	}

	// Check response body
	if w.Body.String() != testContent {
		t.Errorf("expected body %q, got %q", testContent, w.Body.String())
	}
}

func Test_StaticAction_Returns404_ForNonExistentFile(t *testing.T) {
	// Create controller with minimal config
	c := NewController(config.ServerConfig{}, nil)

	// Create test request and response recorder
	req := httptest.NewRequest("GET", "/nonexistent.txt", nil)
	w := httptest.NewRecorder()

	// Call StaticAction
	c.StaticAction(w, req)

	// Check status code
	if w.Code != http.StatusNotFound {
		t.Errorf("expected status code %d, got %d", http.StatusNotFound, w.Code)
	}
}

// symlinkRepoFile makes repoRelPath (relative to the repo root, e.g. "openapi/registry.openapi.yaml")
// available at the same relative path under this test's working directory (controller/) via a
// symlink, mirroring the actual layout at runtime (the real files live at the repo root, but
// `go test` runs with the package directory as its working directory). repoRelPath must have
// exactly one directory component (e.g. "dir/file.ext"). Returns a cleanup func that removes the
// created directory.
func symlinkRepoFile(t *testing.T, repoRelPath string) func() {
	t.Helper()
	target, err := filepath.Abs(filepath.Join("..", repoRelPath))
	if err != nil {
		t.Fatalf("resolve %s: %v", repoRelPath, err)
	}

	dir := filepath.Dir(repoRelPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.Symlink(target, repoRelPath); err != nil {
		t.Fatalf("symlink %s -> %s: %v", repoRelPath, target, err)
	}

	return func() { _ = os.RemoveAll(dir) }
}

func Test_OpenAPISpecAction_ServesVendoredSpec(t *testing.T) {
	defer symlinkRepoFile(t, "openapi/registry.openapi.yaml")()

	c := NewController(config.ServerConfig{}, nil)

	req := httptest.NewRequest("GET", "/openapi.yaml", nil)
	w := httptest.NewRecorder()

	c.OpenAPISpecAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status code %d, got %d", http.StatusOK, w.Code)
	}
	if contentType := w.Header().Get("Content-Type"); contentType != "application/yaml" {
		t.Errorf("expected Content-Type application/yaml, got %s", contentType)
	}
	if !strings.Contains(w.Body.String(), "openapi:") {
		t.Errorf("expected served content to look like an OpenAPI document, got: %s", w.Body.String())
	}
}

func Test_DocsAction_ServesSwaggerUIPage(t *testing.T) {
	defer symlinkRepoFile(t, "static/swagger.html")()

	c := NewController(config.ServerConfig{}, nil)

	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	c.DocsAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status code %d, got %d", http.StatusOK, w.Code)
	}
	if !strings.Contains(w.Body.String(), "SwaggerUIBundle") {
		t.Errorf("expected served content to reference SwaggerUIBundle, got: %s", w.Body.String())
	}
}

func Test_NewController_InitializesCorrectly(t *testing.T) {
	// Create test config and repo
	cfg := config.ServerConfig{
		Hostname: "test-host",
		Port:     8080,
	}
	mockRepo := &MockRepo{}

	// Create controller
	c := NewController(cfg, mockRepo)

	// Check controller fields
	if c.config.Hostname != cfg.Hostname {
		t.Errorf("expected config hostname %s, got %s", cfg.Hostname, c.config.Hostname)
	}
	if c.config.Port != cfg.Port {
		t.Errorf("expected config port %d, got %d", cfg.Port, c.config.Port)
	}
	if c.repo != mockRepo {
		t.Error("repo not set correctly")
	}
}

func (w *errorWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("forced write error")
}
