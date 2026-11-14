# CyberLab Hub

CyberLab Hub is a Go-based cybersecurity learning platform that lets learners discover hands-on challenges, submit answers, track progress, receive release notifications, and discuss topics in real time.

The project is designed to demonstrate five network protocols through connected product features:

- HTTP/JSON for accounts, challenge discovery, submissions, and progress.
- TCP for private progress synchronization between sessions of the same learner.
- UDP for best-effort new-challenge notifications.
- WebSocket for live, room-based discussions.
- gRPC and Protocol Buffers for communication between the backend and a separate Challenge Catalog Service.

## Project status

Phase 4 is implemented. The backend exposes an authenticated HTTP challenge workflow backed by the independent gRPC catalog: learners can search, inspect, submit answers, and review persisted progress, while administrators can publish and archive challenges. Product features are added incrementally according to the roadmap.

## Intended architecture

The system will contain three independently runnable Go programs:

```text
Go tview TUI
  |-- HTTP/JSON ----------> Gin Backend --------> Backend SQLite
  |-- TCP progress sync --> Gin Backend
  |-- UDP notifications --> Gin Backend
  |-- WebSocket chat -----> Gin Backend
                                  |
                                  +-- gRPC --> Challenge Catalog Service
                                                    |
                                                    +--> Catalog SQLite
```

## Core roles

- **Learner:** registers, signs in, browses challenges, submits answers, tracks progress, receives notifications, and joins discussion rooms.
- **Administrator:** imports and reviews challenge content, publishes or archives challenges, and monitors basic service health.
- **Operator:** configures and runs the backend and catalog processes, manages secrets, and backs up local databases.

## Planned challenge categories

- Log analysis
- Network fundamentals
- Digital forensics basics
- Web security fundamentals

All seed challenges and datasets will be local and intentionally created for learning. The project will not execute untrusted user code or deploy intentionally vulnerable public services.

## Technology

- Go 1.25 or later
- Gin
- tview
- SQLite through `database/sql`
- JWT and bcrypt
- Gorilla WebSocket
- grpc-go and Protocol Buffers
- Go `net` package for TCP and UDP

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Roadmap](docs/ROADMAP.md)

## Run the Phase 1 foundation

Requirements: Go 1.26 or later.

Create a local `.env` from `.env.example` and provide a JWT secret containing at least 32 characters. Environment files are intentionally ignored by Git.

Start each service in a separate terminal:

```powershell
$env:JWT_SECRET = "replace-with-a-random-secret-at-least-32-characters"
go run ./cmd/catalog
go run ./cmd/backend
go run ./cmd/tui
```

The backend health endpoint is available at `http://localhost:8080/api/health`. Run the checks with:

```powershell
go test ./...
go vet ./...
go build ./cmd/backend ./cmd/catalog ./cmd/tui
```

## Authentication API

- `POST /api/auth/register` creates a learner account.
- `POST /api/auth/login` returns a signed bearer token.
- `GET /api/me` returns the authenticated identity.
- `GET /api/admin/health` demonstrates administrator-only authorization.

To create the first administrator, set `BOOTSTRAP_ADMIN_USERNAME` and `BOOTSTRAP_ADMIN_PASSWORD` together before starting the backend. The operation is idempotent: it creates the named administrator once and never replaces an existing learner. Remove both variables after the account has been created.

## Challenge catalog gRPC API

The catalog service implements `SearchChallenges`, `GetChallenge`, `VerifyAnswer`, `PublishChallenge`, and `ArchiveChallenge`. Expected answers remain in the catalog database and are never included in public Protocol Buffer messages. Three synthetic challenges are seeded only when the catalog is empty.

The catalog listens on loopback by default because its publication methods are intended only for the trusted backend. Keep it on a private interface unless transport authentication is added.

Protocol Buffer source is stored under `proto/`; generated Go stubs under `gen/` are committed for reproducible builds. To regenerate them without installing `protoc` globally:

```powershell
$env:GOBIN = "$PWD\.cache\bin"
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
$env:PATH = "$env:GOBIN;$env:PATH"
go run github.com/bufbuild/buf/cmd/buf@v1.75.0 generate
```

## Challenge HTTP API

All routes below require a bearer token:

- `GET /api/challenges` searches published challenges; optional query parameters are `query`, `category`, and `difficulty`.
- `GET /api/challenges/:id` returns one published challenge.
- `POST /api/challenges/:id/submissions` verifies `{"answer":"..."}` and records every attempt.
- `GET /api/progress` returns only challenges solved by the authenticated learner.
- `POST /api/admin/challenges` publishes a challenge and is administrator-only.
- `DELETE /api/admin/challenges/:id` archives a challenge and is administrator-only.

Each backend-to-catalog call has a bounded deadline. If the catalog is stopped, the backend returns a controlled `503` or `504` response instead of hanging. Request bodies and gRPC messages are size-limited.

## Security principles

- Passwords are hashed and never logged.
- JWT secrets and local environment files are not committed.
- Answer verification stays on the server side.
- User identity is derived from verified authentication, never a client-supplied user ID.
- Network calls use bounded timeouts and cancellation.
- Private progress events are isolated by authenticated user.

## License

No license has been selected yet.
