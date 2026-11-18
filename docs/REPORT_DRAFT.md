# CyberLab Hub Technical Report - Draft

> Complete the team names, student IDs, approved project title, contribution evidence, screenshots, and final rerun observations before producing `REPORT.pdf`. If the instructor has not approved the CyberLab substitution, this draft must be rewritten for MangaHub.

## 1. System Architecture

CyberLab Hub has three independently runnable Go programs. The tview TUI owns presentation and in-memory session state but never reads a database. The Gin backend owns users, authentication, submissions, solved progress, and the TCP/UDP/WebSocket connection registries. The separate gRPC catalog owns challenge metadata, answer hashes, and publication state. Backend and catalog each use their own SQLite database through `database/sql`.

```text
Go tview TUI
  |-- HTTP/JSON ----------> Gin backend ------> backend SQLite
  |-- TCP progress sync -->     |
  |-- UDP notifications -->     +-- gRPC ----> catalog service --> catalog SQLite
  |-- WebSocket rooms ---->     |
```

Default ports are HTTP 8080, TCP 9090, UDP 9091, and gRPC 9092, all bound to loopback. Environment variables can override them for a controlled lab network.

## 2. Why These Technologies?

### HTTP

Feature -> registration, login, search, submission, progress queries, and administrator publication. Data flow -> the TUI sends JSON with a bearer token; Gin validates identity and input; the backend reads or commits SQLite and calls gRPC when catalog data is required. Why HTTP -> these are discrete request/response operations that benefit from methods, status codes, and inspectable JSON. What can fail -> invalid input, expired tokens, duplicate users, database failures, catalog timeout, or oversized bodies. The implementation returns bounded errors and never trusts a client-supplied user ID.

### TCP

Feature -> a solved challenge is pushed to other connected TUI sessions of the same learner. Data flow -> a TUI authenticates the first newline-delimited message; after an HTTP submission transaction commits, the backend publishes an event to that user's registry while excluding the `X-Session-ID` origin. Why TCP -> it is a long-lived ordered byte stream suitable for private session updates; explicit newline framing is necessary because one read is not one message. What can fail -> missing delimiters, slow readers, duplicate session IDs, disconnects, or invalid JWTs. Size limits, deadlines, buffered writers, and unregister cleanup bound those failures.

### UDP

Feature -> subscribed TUIs receive a best-effort newly published challenge alert. Data flow -> the TUI sends an authenticated subscribe datagram; the backend stores its return address with an expiry; only after gRPC confirms publication does the backend send a public notification datagram. Why UDP -> the event is small, transient, and recoverable through later HTTP search, so connectionless best-effort delivery is an honest fit. What can fail -> loss, expiry, spoofed/invalid registration, or an unreachable receiver. The server does not claim guaranteed delivery and ignores per-recipient send failures.

### WebSocket

Feature -> authenticated live discussion in server-controlled topic rooms. Data flow -> HTTP authentication runs before the upgrade; the URL selects the validated room; a Hub broadcasts server-authored messages containing the verified username. Why WebSocket -> chat needs full-duplex low-latency messages while the TUI remains interactive. What can fail -> cross-room leakage, dead peers, oversized frames, unsafe concurrent writes, or slow clients. Per-client writer pumps, room registries, ping/pong deadlines, message limits, and disconnect cleanup address those risks.

### gRPC

Feature -> the Gin backend uses a separate catalog process for search, details, answer verification, publication, and archive. Data flow -> the backend uses generated protobuf stubs with a per-call deadline; the catalog validates requests and owns its SQLite data. Why gRPC -> this creates a typed service boundary and generated client/server contract rather than an in-process call. What can fail -> invalid IDs, stopped catalog, deadline expiry, or storage errors. The service returns gRPC status codes and Gin maps unavailable/deadline errors to controlled HTTP responses.

## 3. Implementation and Verification

### Solve flow

1. TUI submits an answer through authenticated HTTP and includes its TCP session ID.
2. Gin derives the user from the JWT and calls `VerifyAnswer` through gRPC with a deadline.
3. The catalog loads only a published challenge and compares the normalized answer against bcrypt-protected verification material.
4. The backend inserts the attempt and solved progress in one SQLite transaction.
5. Only after commit, the TCP registry sends a progress event to other sessions of the same user.

### Publication flow

1. Gin authenticates an administrator and validates the HTTP body size.
2. Gin calls `PublishChallenge` through generated gRPC stubs.
3. The catalog validates, hashes the answer, commits the challenge, and responds with public fields only.
4. After success, the UDP registry sends a best-effort notification to non-expired subscribers.
5. A missed datagram does not lose source-of-truth state because HTTP search reads the catalog later.

Use the compact observed-results table from `docs/VERIFICATION.md` in the final PDF. Add one architecture diagram and two sequence diagrams that match the code and actual ports.

## 4. Challenges, Reflection, and Contributions

### Defect investigated

During dependency setup, `go mod tidy` was run before the new SQLite and JWT imports existed. Go correctly removed those unused requirements, and the next test failed with “no required module provides package.” The cause was ordering, not a network or library defect. The fix was to add the implementation files, add the pinned dependencies again, run `go mod tidy`, and rerun all tests. The retest passed.

Another verification correction occurred when Buf lint rejected `cyberlab.catalog.v1` because its package directory was `catalog/v1`. The package and generated service name were aligned, stubs regenerated, and lint plus RPC tests rerun successfully.

Phase 9 review also found that sending a WebSocket message directly from a tview button callback could block the UI for the socket write timeout. Sending now runs in a background goroutine, with error feedback dispatched through `QueueUpdateDraw`. The room name is captured on the UI thread before the background handshake. A real WebSocket client test covers sending, receiving, and cancellation; final interactive terminal review is still required.

The initial vulnerability scan reported 14 reachable findings in Go 1.27.1 and quic-go v0.59.0. Selecting Go 1.27.4 for the project and updating quic-go to v0.59.1 removed the reachable findings in the rerun. Three module-level advisories remain, but the scan did not find calls into their affected code. Test, vet, and build also passed on the patched toolchain.

### Known limitations

- The default setup is localhost-only and does not provide TLS for public deployment.
- UDP delivery is intentionally not guaranteed.
- No rate limiter or account lockout is included.
- The race detector was not runnable on the current Windows host because `gcc` was unavailable.
- CyberLab is a domain substitution and requires instructor approval against the supplied MangaHub specification.

### Contributions

- Member 1 - `[name / student ID]`: `[specific commits, code areas, tests, demo explanation]`
- Member 2 - `[name / student ID]`: `[specific commits, code areas, tests, demo explanation]`

Record actual work; do not assign percentages or claims that the commit history and live explanation cannot support.

### Three meaningful AI-assisted interactions

1. Architecture: AI helped compare candidate domains and map the five protocols to connected CyberLab features. The team must record the instructor's decision on whether this replacement is acceptable.
2. Implementation/debugging: AI proposed the Go package structure and helped diagnose the dependency-order and protobuf-package lint failures. The suggestions were accepted only after the corrected tests and lint passed.
3. Verification/review: AI identified that `git clean -d --interactive` would target two untracked Phase 4 source files; a dry run confirmed the risk, so the destructive command was rejected and the tested files were committed first. AI also recommended race testing; the attempt failed because the host lacked `gcc`, so the report records the limitation rather than claiming a pass.

The final report should remain 4-5 pages of main content, excluding cover, references, and short appendices. Replace every placeholder and use fresh observed results from the final machine.
