# System Architecture

## Design goals

CyberLab Hub is a small, explainable distributed system for two student developers. Each protocol has one observable product responsibility. The first release prioritizes correctness, isolation, failure handling, and reproducible demonstrations over feature count.

## Processes and data ownership

### TUI client

The TUI is a `tview` application. It never reads SQLite files directly. It communicates only through the backend's public network interfaces.

Planned screens:

- Login and registration
- Challenge browser
- Challenge details and answer submission
- My learning progress
- Discussion rooms
- Notifications
- Connection status and help
- Administrator challenge publishing

### Gin backend

The backend owns:

- Users and password hashes
- Authentication and authorization
- Submission history
- Per-user challenge progress
- TCP session registry
- UDP subscription registry
- WebSocket room hub

The backend does not read the catalog database directly. Challenge discovery, details, answer verification, and publishing cross the gRPC boundary.

### Challenge Catalog Service

The catalog is a separate Go process with its own SQLite database. It owns challenge content, publication state, answer verification material, categories, and difficulty.

Planned unary RPCs:

- `SearchChallenges`
- `GetChallenge`
- `VerifyAnswer`
- `PublishChallenge`
- `ArchiveChallenge`

## Protocol responsibilities

| Protocol | Product responsibility | Main failure to demonstrate |
| --- | --- | --- |
| HTTP | Accounts, challenge browsing, submissions, progress queries | Invalid input, unauthorized request, persistence after restart |
| TCP | Private progress synchronization between sessions of one learner | Framing, disconnect cleanup, cross-user isolation |
| UDP | Best-effort notification that a challenge was published | Lost/unavailable receiver does not crash the backend |
| WebSocket | Live discussion scoped to a server-controlled room | Room isolation and disconnected-client cleanup |
| gRPC | Backend-to-catalog typed service calls | Deadline-bounded failure when the catalog is stopped |

## Primary learning flow

1. The learner signs in through HTTP and receives a JWT.
2. The TUI requests a challenge through HTTP.
3. The backend calls the catalog through a generated gRPC client with a deadline.
4. The learner submits an answer through HTTP.
5. The backend asks the catalog to verify the answer.
6. After a correct answer, the backend commits progress to SQLite.
7. Only after the commit succeeds, the backend sends a TCP progress event to other authenticated sessions of the same learner.

## Challenge publishing flow

1. An administrator requests publication through a protected HTTP endpoint.
2. The backend checks the administrator role.
3. The backend calls `PublishChallenge` through gRPC.
4. After gRPC confirms publication, the backend sends a UDP notification to subscribed clients.
5. A learner who misses the datagram still sees the published challenge during the next HTTP search.

## Security boundaries

- Challenge responses never expose expected answers or verification secrets.
- Passwords are hashed with bcrypt.
- JWTs are not accepted from query strings except where a WebSocket client limitation is explicitly documented and mitigated.
- TCP and UDP registration messages require authenticated session material.
- The server derives the learner identity from verified authentication.
- Database queries are parameterized.
- Authentication attempts, message sizes, and request durations are bounded.
- Seed content uses synthetic data and does not include real credentials or malicious payloads.

## Initial repository layout

```text
cmd/
  tui/
  backend/
  catalog/
internal/
  auth/
  backend/
  catalog/
  challenge/
  progress/
  tcp/
  udp/
  websocket/
  tui/
proto/
gen/
migrations/
seed/
tests/
docs/
```

The exact package boundaries may be adjusted after the first vertical slice, but the three-process boundary and data ownership will remain stable.
