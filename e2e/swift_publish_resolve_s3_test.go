//go:build e2e
// +build e2e

// Package e2e: Swift CLI E2E test for the S3-backed registry.
// Run with: make test-e2e-swift-s3 (requires AWS SSO login, e.g. ./aws_login.sh, and Swift toolchain).
// Unlike TestSwiftPublishResolve (Maven-backed), this drives a registry started with repo.type=s3
// against a real S3 bucket, using the actual swift CLI for both publish and dependency resolution.
// The bucket is cleared before the test runs but deliberately left populated afterward, so the
// published package files can be inspected in S3 (e.g. via the console or `aws s3 ls`).
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const s3E2ERegistryURL = "http://127.0.0.1:8083"

// s3E2ETestConfig returns bucket/region/profile for the S3 E2E test. Defaults match the
// "spm-data" bucket provisioned via spm_registry/terraform on the "aws-spielwiese" profile;
// override via S3_TEST_BUCKET, S3_TEST_REGION, S3_TEST_PROFILE (same env vars as make test-s3-integration).
func s3E2ETestConfig() (bucket string, region string, profile string) {
	bucket = os.Getenv("S3_TEST_BUCKET")
	if bucket == "" {
		bucket = "spm-data-286123791725-eu-central-1-an"
	}
	region = os.Getenv("S3_TEST_REGION")
	if region == "" {
		region = "eu-central-1"
	}
	profile = os.Getenv("S3_TEST_PROFILE")
	if profile == "" {
		profile = "aws-spielwiese"
	}
	return
}

func newS3E2EClient(t *testing.T, region string, profile string) *s3.Client {
	t.Helper()
	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}
	return s3.NewFromConfig(cfg)
}

// checkS3Access skips the test (rather than failing) when the bucket is not reachable,
// mirroring waitForMaven's behavior for the Maven-backed E2E test.
func checkS3Access(t *testing.T, client *s3.Client, bucket string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Skipf("S3 bucket %q not reachable (check AWS SSO login, e.g. ./aws_login.sh): %v", bucket, err)
	}
}

// cleanupS3Prefix removes every object under prefix (pass "" to clear the whole bucket).
// Best-effort: failures are logged, not fatal.
func cleanupS3Prefix(t *testing.T, client *s3.Client, bucket string, prefix string) {
	t.Helper()
	ctx := context.Background()
	var continuationToken *string
	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			t.Logf("cleanup: list objects under %q failed: %v", prefix, err)
			return
		}
		for _, obj := range out.Contents {
			if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: obj.Key}); err != nil {
				t.Logf("cleanup: delete %s failed: %v", aws.ToString(obj.Key), err)
			}
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			return
		}
		continuationToken = out.NextContinuationToken
	}
}

// TestSwiftPublishResolveS3 runs the registry with repo.type=s3 against a real bucket and drives
// it with the actual swift CLI: publish (via `swift package-registry publish`), HTTP verification,
// and full dependency resolution + build (via `swift package resolve` / `swift build` / `swift run`)
// from a minimal consumer package. This proves the S3 backend works for real SPM usage, not just
// the direct Go-level checks in integration_test.go.
func TestSwiftPublishResolveS3(t *testing.T) {
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

	bucket, region, profile := s3E2ETestConfig()
	client := newS3E2EClient(t, region, profile)
	checkS3Access(t, client, bucket)

	// Clear the whole bucket before running so only this run's objects are present afterward.
	// Deliberately not cleaned up at the end, so the published files can be inspected in S3.
	cleanupS3Prefix(t, client, bucket, "")

	const prefix = "e2e-swift"

	configYAML := fmt.Sprintf(`server:
  hostname: 127.0.0.1
  port: 8083
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
	configPath := filepath.Join(t.TempDir(), "config.e2e.s3.yml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	samplePkgDir := filepath.Join(root, "testdata", "e2e", "example.SamplePackage")
	utilsPkgDir := filepath.Join(root, "testdata", "e2e", "example.UtilsPackage")

	env := &e2eEnv{
		rootDir:      root,
		configPath:   configPath,
		registryURL:  s3E2ERegistryURL,
		samplePkgDir: samplePkgDir,
		utilsPkgDir:  utilsPkgDir,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}

	const scope = "example"

	// Clean fixture build state so publish uses current source, not stale build artifacts.
	for _, dir := range []string{samplePkgDir, utilsPkgDir} {
		os.RemoveAll(filepath.Join(dir, ".build"))
		os.RemoveAll(filepath.Join(dir, ".swiftpm"))
	}

	defer startRegistryServer(t, env)()

	t.Run("Publish", func(t *testing.T) {
		for _, pkg := range []struct{ name, dir string }{
			{"SamplePackage", samplePkgDir},
			{"UtilsPackage", utilsPkgDir},
		} {
			pkgID := scope + "." + pkg.name
			for _, ver := range []string{"1.0.0", "1.1.0"} {
				if out, err := runSwift(t, pkg.dir, "package", "dump-package"); err == nil {
					os.WriteFile(filepath.Join(pkg.dir, "Package.json"), []byte(out), 0644)
				}
				out, err := runSwift(t, pkg.dir, "package-registry", "publish", pkgID, ver,
					"--url", env.registryURL, "--allow-insecure-http")
				if err != nil {
					t.Fatalf("publish %s %s: %v\n%s", pkgID, ver, err, out)
				}
			}
		}
	})

	t.Run("VerifyMetadataFromS3", func(t *testing.T) {
		req, _ := http.NewRequest("GET", env.registryPath(scope, "SamplePackage", "1.0.0"), nil)
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
		var info map[string]any
		if err := json.Unmarshal(body, &info); err != nil {
			t.Fatalf("parse version info: %v", err)
		}
		resources, _ := info["resources"].([]any)
		if len(resources) == 0 {
			t.Fatal("no resources in release metadata")
		}
		r0, _ := resources[0].(map[string]any)
		checksum, _ := r0["checksum"].(string)
		if len(checksum) != 64 {
			t.Fatalf("expected a 64-char sha256 checksum (from S3 object metadata), got %q", checksum)
		}
	})

	t.Run("VerifyListReleases", func(t *testing.T) {
		req, _ := http.NewRequest("GET", env.registryPath(scope, "SamplePackage"), nil)
		req.Header.Set("Accept", acceptJSON)
		resp, err := env.httpClient.Do(req)
		if err != nil {
			t.Fatalf("list releases: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		for _, v := range []string{"1.0.0", "1.1.0"} {
			if !bytes.Contains(body, []byte(`"`+v+`"`)) {
				t.Fatalf("list response missing version %s: %s", v, body)
			}
		}
	})

	t.Run("VerifyCollectionFromS3", func(t *testing.T) {
		req, _ := http.NewRequest("GET", env.registryPath("collection", scope), nil)
		req.Header.Set("Accept", "application/json")
		resp, err := env.httpClient.Do(req)
		if err != nil {
			t.Fatalf("get scope collection: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		for _, pkg := range []string{"example.SamplePackage", "example.UtilsPackage"} {
			if !bytes.Contains(body, []byte(pkg)) {
				t.Fatalf("scope collection missing %s: %s", pkg, body)
			}
		}
	})

	// ConsumerResolve exercises the full real-world path: swift package-registry set, then swift
	// package resolve/build/run, resolving actual dependencies from the S3-backed registry. Uses a
	// dedicated, minimal consumer (only SamplePackage + UtilsPackage) rather than the shared
	// testdata/e2e/Consumer fixture, which also depends on example.SwiftSignedPkg (needs the
	// optional spm-extended signing plugin) and isn't relevant to verifying the S3 backend.
	t.Run("ConsumerResolve", func(t *testing.T) {
		consumerDir := t.TempDir()
		packageSwift := `// swift-tools-version:6.0
import PackageDescription

let package = Package(
    name: "S3Consumer",
    platforms: [.macOS(.v12)],
    dependencies: [
        .package(id: "example.SamplePackage", from: "1.0.0"),
        .package(id: "example.UtilsPackage", from: "1.0.0"),
    ],
    targets: [
        .executableTarget(
            name: "S3Consumer",
            dependencies: [
                .product(name: "SamplePackage", package: "example.SamplePackage"),
                .product(name: "UtilsPackage", package: "example.UtilsPackage"),
            ]
        ),
    ]
)
`
		if err := os.WriteFile(filepath.Join(consumerDir, "Package.swift"), []byte(packageSwift), 0644); err != nil {
			t.Fatalf("write Package.swift: %v", err)
		}
		sourcesDir := filepath.Join(consumerDir, "Sources", "S3Consumer")
		if err := os.MkdirAll(sourcesDir, 0755); err != nil {
			t.Fatalf("mkdir Sources: %v", err)
		}
		mainSwift := `import SamplePackage
import UtilsPackage

print("Resolved SamplePackage: \(SamplePackage.self)")
print("Resolved UtilsPackage: \(UtilsPackage.self)")
`
		if err := os.WriteFile(filepath.Join(sourcesDir, "main.swift"), []byte(mainSwift), 0644); err != nil {
			t.Fatalf("write main.swift: %v", err)
		}

		if out, err := runSwift(t, consumerDir, "package-registry", "set", env.registryURL, "--allow-insecure-http"); err != nil {
			t.Fatalf("swift package-registry set: %v\n%s", err, out)
		}

		out, err := runSwift(t, consumerDir, "package", "resolve")
		if err != nil {
			t.Fatalf("swift package resolve: %v\n%s", err, out)
		}

		resolvedPath := filepath.Join(consumerDir, "Package.resolved")
		content, err := os.ReadFile(resolvedPath)
		if err != nil {
			t.Fatalf("read Package.resolved: %v", err)
		}
		for _, pkg := range []string{"example.SamplePackage", "example.UtilsPackage"} {
			if !bytes.Contains(content, []byte(pkg)) {
				t.Fatalf("Package.resolved missing %s: %s", pkg, content)
			}
		}

		buildOut, err := runSwift(t, consumerDir, "build")
		if err != nil {
			t.Fatalf("swift build: %v\n%s", err, buildOut)
		}

		runOut, err := runSwift(t, consumerDir, "run", "S3Consumer")
		if err != nil {
			t.Fatalf("swift run: %v\n%s", err, runOut)
		}
		if !strings.Contains(runOut, "Resolved SamplePackage") || !strings.Contains(runOut, "Resolved UtilsPackage") {
			t.Fatalf("consumer output missing expected lines: %s", runOut)
		}
	})
}
