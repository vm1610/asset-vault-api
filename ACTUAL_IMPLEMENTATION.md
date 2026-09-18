# Actual Implementation

This document is the source of truth for what was actually built and
verified in this project. It exists specifically so that resume/portfolio
copy derived from this project never claims more than what is here.

## Real stack used

- Go 1.27.1
- Gin (`github.com/gin-gonic/gin`) for HTTP routing and middleware
- `github.com/golang-jwt/jwt/v5` for RS256 JWT parsing/validation
- A hand-written JWKS client (fetch + cache by `kid`, no third-party JWKS library)
- **SQLite**, not Postgres, via the pure-Go driver `modernc.org/sqlite` through
  `database/sql`. This is a deliberate substitution: the build environment
  had no Docker and no running Postgres instance, so Postgres was never
  actually used or tested. The SQL in `internal/db` avoids SQLite-only
  syntax where practical, but Postgres compatibility was never verified —
  do not claim Postgres experience from this project.
- `github.com/aws/aws-sdk-go-v2` (`service/s3`, `config`) for the S3 storage
  backend — real SDK code, but see "What was NOT verified" below.
- `golang.org/x/image/draw` for thumbnail resizing (Catmull-Rom scaling)
- `github.com/google/uuid` for ID generation

## Features actually implemented and working

1. **REST API** (`internal/api`): `POST /assets` (multipart upload),
   `GET /assets` (list, scoped to the caller), `GET /assets/:id`,
   `DELETE /assets/:id`, `GET /jobs/:id` (poll job status),
   `GET /download/*key` (signed download for the local backend).
2. **JWT/JWKS auth** (`internal/auth`): Gin middleware validates RS256
   tokens against a live-fetched, cached JWKS. Rejects missing headers,
   expired tokens, tokens signed by an unrelated key (tampered signature),
   unknown `kid`, and (when configured) wrong audience/issuer. Verified by
   6 tests using a real `httptest` server and real generated RSA keypairs —
   not mocked assertions.
3. **Streaming multipart upload**: the uploaded file is streamed to a temp
   file on disk (never buffered whole in process memory), then streamed
   from disk into the storage backend. This is a real streaming pattern,
   though it is disk-staged, not a single zero-copy stream straight from
   the HTTP body to S3.
4. **Storage abstraction** (`internal/storage`): a `Storage` interface with
   two implementations:
   - `LocalStorage`: filesystem-backed, with HMAC-SHA256-signed,
     time-limited download URLs served by a dedicated `/download` handler —
     genuinely exercised end-to-end, including signature tampering and
     expiry rejection tests.
   - `S3Storage`: real AWS SDK v2 calls (`PutObject`, `GetObject`,
     `DeleteObject`, `PresignGetObject`), unit-tested against an in-memory
     fake implementing the same narrow client interface the code depends
     on. **Never run against a real S3 bucket** (no valid AWS credentials
     were available in the build environment).
5. **Background worker** (`internal/worker`): a channel-based goroutine
   pool with panic recovery per task. On upload, a job row is created
   (`pending`); a worker picks it up, marks it `processing`, decodes the
   image, resizes it (capped at 256px on the long edge, no upscaling),
   re-encodes as JPEG, stores it via the `Storage` interface, records the
   thumbnail key on the asset, and marks the job `completed` or `failed`
   with an error message. Verified with real generated JPEG/PNG images.
6. **Ownership isolation**: assets and jobs are scoped by the JWT `sub`
   claim; a second user's token gets 404s, not 403s, for another user's
   assets (tested).
7. **Mock JWKS dev issuer** (`cmd/mockjwks`): a small standalone binary that
   generates an RSA keypair, serves it as a JWKS document, and mints test
   tokens. Explicitly documented in its own file header as dev-only and not
   an OIDC provider.

## What was actually run and verified (not just claimed)

- `go build ./...`, `go vet ./...`, `gofmt -l .` (clean), `go mod tidy` — all
  run for real, in this environment.
- `go test ./...` — 22 tests across `auth`, `storage`, `db`, `worker`, `api`,
  all passing, all against real dependencies in-process (real SQLite files,
  real filesystem I/O, real RSA-signed JWTs, real JPEG/PNG encode-decode).
  No mocked assertions beyond the S3 client fake described above.
- The compiled server binary (`go build -o server ./cmd/server`) and the
  compiled `mockjwks` binary were **run as real separate OS processes** and
  driven end-to-end with `curl`: mint token → upload a real JPEG →
  poll job status to `completed` → fetch the asset (got back a populated
  `download_url` and `thumbnail_url`) → download via the signed URL (byte
  count matched the upload exactly) → list → delete → confirm 404
  afterward. This is the strongest evidence in this project and the one to
  cite in interviews.

## What was NOT verified — do not claim these

- **Postgres**: never used. Only SQLite was ever run. If asked about the
  data layer, describe it as SQLite via `database/sql`, and be honest that
  a Postgres migration was designed for but not executed or tested.
- **Live S3**: the `S3Storage` backend compiles and passes unit tests
  against a fake client, but was never exercised against an actual AWS
  bucket, so nothing about real multipart-to-S3 latency, IAM permission
  edge cases, or presigned URL behavior against real AWS has been
  confirmed. Say "wrote real SDK v2 integration code, unit-tested it, did
  not have AWS credentials to validate against a live bucket."
- **Docker / docker-compose**: `Dockerfile`, `Dockerfile.mockjwks`, and
  `docker-compose.yml` exist and were written to be correct, but Docker was
  not installed in the build environment, so none of them were ever built
  or run. Do not claim "containerized and deployed" — claim "wrote
  Dockerfiles, never built them."
- **Kubernetes, CI/CD**: not built at all.
- **A real OIDC/IdP integration**: `mockjwks` is a self-signed dev stand-in,
  not Auth0/Cognito/Okta/etc. The JWKS *validation* logic is real and
  tested; the *issuance* side is a mock.
- **Video/non-image assets**: uploads of any content type are accepted and
  stored, but thumbnail generation only supports `image/jpeg` and
  `image/png`; other types are stored without a thumbnail and the job is
  marked `completed` with no thumbnail rather than `failed` (this is
  intentional — see `internal/worker/processor.go`).
- **Large-scale concurrency/load testing**: the worker pool and streaming
  upload were exercised with small test files and a handful of concurrent
  goroutines in tests, not load-tested at any real volume.

## How to run it

```bash
go test ./...          # 22 tests, all passing
go vet ./...
gofmt -l .              # should print nothing

go run ./cmd/mockjwks               # terminal 1
go run ./cmd/server                 # terminal 2 (reads .env / env vars)
```

See `README.md` for the full endpoint list and a `curl`-driven walkthrough.
