# Integration & E2E Tests

Integration tests use a real Maven server (Nexus or Reposilite in Docker). E2E tests add Swift publish/resolve and registry API checks.

**Prerequisites:** Docker, Go 1.23+. For E2E Swift: Swift 5.9+, Maven server up (or use `test-e2e-full`).

## Quick run

| Goal | Command |
|------|--------|
| Integration (Nexus) | `make test-integration` |
| Integration (Reposilite) | `MAVEN_PROVIDER=reposilite make test-integration` |
| E2E Swift + registry (start/stop server) | `make test-e2e-full` or `MAVEN_PROVIDER=reposilite make test-e2e-full` |
| E2E with server already up | `make test-integration-up` then `make test-e2e-swift` and/or `make test-e2e-registry` |
| Stop server | `make test-integration-down` |

**MAVEN_PROVIDER:** `nexus` (default, port 8081) or `reposilite` (port 8080). Bootstrap creates repo `private` and credentials; Reposilite token written to `.reposilite-test-token`.

## E2E overview

- Builds registry with `config.e2e.yml` / `config.e2e.reposilite.yml` (port 8082), publishes `example.SamplePackage@1.0.0` via Swift CLI, verifies metadata, manifests, collections, and consumer resolve.
- HTTPS: `make test-e2e-generate-certs` then `E2E_REGISTRY_URL=https://127.0.0.1:8082 make test-e2e-swift`.

## S3 backend tests

Require AWS SSO login (e.g. `./aws_login.sh`); default bucket/region/profile match the `spm-data`
bucket provisioned via `spm_registry/terraform` (override with `S3_TEST_BUCKET`, `S3_TEST_REGION`,
`S3_TEST_PROFILE`).

| Goal | Command |
|------|--------|
| Direct Go-level repo/s3 checks | `make test-s3-integration` |
| Swift CLI publish + resolve/build/run against S3 | `make test-e2e-swift-s3` (clears its bucket prefix before running, leaves the published files for inspection afterward) |
| Publish a binaryTarget package (real .xcframework) to S3 | `make test-e2e-publish-binarytarget-s3` (documents that this currently succeeds even though the archive is tens of MB, since `publish.maxSize` isn't enforced anywhere) |

## OpenAPI conformance checking

E2E tests that talk to the registry over Go's `http.Client` (all three S3 tests above, plus the
Maven-backed `test-e2e-swift`/`test-e2e-registry`) run every request/response pair through
`apivalidate`, which checks them against a vendored copy of the official Swift Package Registry
OpenAPI spec (`openapi/registry.openapi.yaml`, from `swiftlang/swift-package-manager`). Any
deviation is logged as a `slog.Warn`, not a test failure — this is a diagnostic aid for spec drift,
not a correctness gate. Requests to this server's own `/collection` extension (not part of the
official spec) are silently skipped. See `apivalidate/apivalidate.go` for details, including the
known Go-regexp-vs-ECMA-262 limitation around the Scope/PackageName parameter patterns.

## Config reference

| Env | Purpose |
|-----|--------|
| `INTEGRATION_TESTS=1` | Enable integration tests |
| `MAVEN_PROVIDER` | `nexus` or `reposilite` |
| `MAVEN_REPO_URL` | e.g. `http://localhost:8081/repository` (Nexus) or `http://localhost:8080` (Reposilite) |
| `MAVEN_REPO_NAME` | `private` |
| `MAVEN_REPO_USERNAME` / `MAVEN_REPO_PASSWORD` | Nexus: `admin` / from `.nexus-test-password` or `admin123`; Reposilite: `e2e` / from `.reposilite-test-token` or `test-secret` |
| `NEXUS_WAIT_TIMEOUT` | Seconds to wait for server (default 180; CI often 360) |

Config files: `config.e2e.yml` (Nexus), `config.e2e.reposilite.yml`, `config.e2e.https.yml`. Docker: `docker-compose.test.yml` (services `nexus`, `reposilite`).

## Writing integration tests

Use build tag `integration`, call `SkipIfNotIntegration(t)`, and use `IntegrationTestHelper` from `repo/maven/integration_test.go` (e.g. `WaitForServer`, `GetMavenConfig`). Example:

```go
//go:build integration
// +build integration

func TestMyIntegration(t *testing.T) {
    SkipIfNotIntegration(t)
    helper := NewIntegrationTestHelper(os.Getenv("MAVEN_REPO_URL"))
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()
    if err := helper.WaitForServer(ctx, 2*time.Minute); err != nil { t.Fatal(err) }
    cfg := helper.GetMavenConfig()
    // ...
}
```

## Troubleshooting & CI

- **Port in use:** `lsof -i :8081` or `:8080`. Stop with `make test-integration-down`; full cleanup: `docker-compose -f docker-compose.test.yml down` and `rm -rf nexus-data/ reposilite-data/`.
- **Timeouts:** Increase timeout in test or set `NEXUS_WAIT_TIMEOUT` (e.g. 360 in CI). See `.github/workflows/integration.yml` and `.github/workflows/e2e.yml`.
