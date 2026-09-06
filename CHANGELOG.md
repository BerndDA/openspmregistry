# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Latest]

- Added S3 repository backend for storing packages in Amazon S3 (or S3-compatible services)
- Added `make test-e2e-swift-s3`: Swift CLI E2E test (publish + resolve/build/run) against the S3 backend
- Added `make test-e2e-publish-binarytarget-s3`: documents that publishing a package with local binaryTarget dependencies (large archives) currently succeeds, since `publish.maxSize` is not enforced anywhere
- Added `apivalidate`: checks real E2E HTTP request/response pairs against a vendored copy of the official Swift Package Registry OpenAPI spec (`openapi/registry.openapi.yaml`), logging any conformance deviation as a warning rather than failing tests

## [0.2.0] - 2026-03-22

- Added Maven repository backend for storing packages in Maven-compatible servers (e.g. Nexus, Reposilite) [#30](https://github.com/wgr1984/openspmregistry/issues/30) ([#31](https://github.com/wgr1984/openspmregistry/pull/31))

## [0.1.0] - 2026-01-10
- Added Package Collections feature [#25](https://github.com/wgr1984/openspmregistry/issues/25)
- Add prev/next version link in header [#19](https://github.com/wgr1984/openspmregistry/issues/19)

## [0.0.2] - 2025-04-21
- Docker build and publish workflow
- Automated release process
- Unit test + coverage
- Bugfixes

## [0.0.1] - 2025-01-06
- First release: Simple, fast and as feature complete as possible version of Swift Package Manager Registry. (at least in terms of API)
- See: https://wgr1984.github.io/docs/openspmregistry/

