//go:build e2e
// +build e2e

// Package e2e: publishes testdata/e2e/example.binarytarget (a package with local binaryTarget
// dependencies, i.e. real .xcframework binaries bundled via `path:` rather than a remote `url:`)
// to the S3-backed registry via the real swift CLI.
//
// This documents CURRENT behavior, which may be surprising: publishing succeeds. `publish.maxSize`
// (200KB by default) is defined in config.ServerConfig but is not enforced anywhere in
// controller/publish.go, so a package whose archive is tens of megabytes (because it embeds
// compiled binaries) uploads without error, on any backend (file/maven/s3). If size enforcement is
// added later, update this test to assert the publish fails instead.
//
// The bucket prefix is cleared before the test runs but deliberately left populated afterward, so
// the published archive can be inspected in S3 (e.g. via the console or `aws s3 ls`).
package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPublishBinaryTargetS3 publishes example.binarytarget to the S3-backed registry and asserts
// that publish currently succeeds despite the archive being far larger than publish.maxSize.
func TestPublishBinaryTargetS3(t *testing.T) {
	if os.Getenv("E2E_TESTS") == "" {
		t.Skip("Skipping E2E test. Set E2E_TESTS=1 to run.")
	}
	if !swiftAvailable() {
		t.Skip("Swift toolchain not found. Install Swift to run this test.")
	}

	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	pkgDir := filepath.Join(root, "testdata", "e2e", "example.binarytarget")
	if _, err := os.Stat(pkgDir); err != nil {
		t.Skipf("fixture %s not present: %v", pkgDir, err)
	}

	bucket, region, profile := s3E2ETestConfig()
	client := newS3E2EClient(t, region, profile)
	checkS3Access(t, client, bucket)

	// Own prefix, cleared before running (not after): the published archive is deliberately left
	// in S3 for inspection, same as TestSwiftPublishResolveS3. A dedicated prefix keeps this from
	// clobbering that other test's own left-behind files (which share the same bucket).
	const prefix = "e2e-binarytarget"
	cleanupS3Prefix(t, client, bucket, prefix)

	const scope = "example"
	const pkgName = "SMSSDK"
	pkgID := scope + "." + pkgName

	configYAML := fmt.Sprintf(`server:
  hostname: 127.0.0.1
  port: 8084
  tlsEnabled: false
  certs:
    cert: server.crt
    key: server.key
  repo:
    type: s3
    s3:
      bucket: %s
      region: %s
      profile: %s
      prefix: %s
  publish:
    maxSize: 204800
  auth:
    enabled: false
  packageCollections:
    enabled: true
    requirePackageJson: false
`, bucket, region, profile, prefix)
	configPath := filepath.Join(t.TempDir(), "config.e2e.binarytarget.yml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	env := &e2eEnv{
		rootDir:     root,
		configPath:  configPath,
		registryURL: "http://127.0.0.1:8084",
		// Archive/upload of the real xcframework binaries is tens of MB; allow more time than the
		// default 30s used by the other S3 E2E test.
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}

	os.RemoveAll(filepath.Join(pkgDir, ".build"))
	os.RemoveAll(filepath.Join(pkgDir, ".swiftpm"))
	os.Remove(filepath.Join(pkgDir, "Package.json"))

	defer startRegistryServer(t, env)()

	if out, err := runSwift(t, pkgDir, "package", "dump-package"); err == nil {
		os.WriteFile(filepath.Join(pkgDir, "Package.json"), []byte(out), 0644)
	} else {
		t.Fatalf("dump-package: %v\n%s", err, out)
	}

	out, err := runSwift(t, pkgDir, "package-registry", "publish", pkgID, "1.0.0",
		"--url", env.registryURL, "--allow-insecure-http")
	if err != nil {
		t.Fatalf("expected publish to currently succeed (publish.maxSize is not enforced), but it failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "successfully published") {
		t.Fatalf("expected a success message from swift package-registry publish, got: %s", out)
	}

	// Confirm the oversized archive actually landed in S3 and is servable back.
	req, _ := http.NewRequest("GET", env.registryPath(scope, pkgName, "1.0.0"), nil)
	req.Header.Set("Accept", acceptJSON)
	resp, err := env.httpClient.Do(req)
	if err != nil {
		t.Fatalf("get metadata: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
}
