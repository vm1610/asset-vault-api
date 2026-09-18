# asset-vault-api

A Go/Gin REST API for uploading, cataloging, and processing media assets.
Uploads are authenticated with JWKS-validated JWTs, streamed to pluggable
object storage (local filesystem or S3), and handed to a background worker
pool that generates thumbnails and reports job status back to the client.

## Features

- JWT bearer authentication validated against a JSON Web Key Set (RS256),
  with per-request audience/issuer checks
- Multipart file upload streamed to storage without buffering the full file
  in memory
- Pluggable object storage: local filesystem (with HMAC-signed, time-limited
  download URLs) or S3 (via the AWS SDK v2)
- Background worker pool that generates JPEG thumbnails for uploaded images
  and exposes job status through a polling endpoint
- Per-owner asset isolation: a caller can only see and manage assets tied to
  their own token subject

## Architecture

```
client -> [JWT middleware] -> Gin handlers -> SQLite (assets, jobs)
                                    |
                                    v
                              Storage interface
                              /              \
                        LocalStorage        S3Storage
                                    |
                                    v
                           worker pool -> thumbnail generation
```

## Running locally

Requires Go 1.27+.

```bash
go mod download

# terminal 1: start the mock JWKS issuer (dev-only, not a real IdP)
go run ./cmd/mockjwks

# terminal 2: start the API
cp .env.example .env
export $(cat .env | xargs)
go run ./cmd/server
```

Mint a token and call the API:

```bash
TOKEN=$(curl -s "http://localhost:9091/token?sub=demo-user" | jq -r .access_token)

curl -X POST http://localhost:8080/assets \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@photo.jpg;type=image/jpeg"

curl http://localhost:8080/assets -H "Authorization: Bearer $TOKEN"
```

## API

| Method | Path              | Description                                   |
|--------|-------------------|------------------------------------------------|
| GET    | `/healthz`        | Liveness check                                 |
| POST   | `/assets`         | Upload an asset (`multipart/form-data`, field `file`) |
| GET    | `/assets`         | List the caller's assets                       |
| GET    | `/assets/:id`     | Get one asset, including a presigned download URL |
| DELETE | `/assets/:id`     | Delete an asset and its thumbnail               |
| GET    | `/jobs/:id`       | Poll the status of a processing job             |
| GET    | `/download/*key`  | Signed download endpoint (local storage only)   |

All routes except `/healthz` and `/download` require `Authorization: Bearer <token>`.

## Configuration

See `.env.example` for the full list of environment variables, including
`STORAGE_BACKEND` (`local` or `s3`), `JWKS_URL`, and `WORKER_COUNT`.

## Tests

```bash
go test ./...
go vet ./...
gofmt -l .
```

## Docker

A `Dockerfile` (API), `Dockerfile.mockjwks` (dev JWKS issuer), and
`docker-compose.yml` are included for local container-based development.
