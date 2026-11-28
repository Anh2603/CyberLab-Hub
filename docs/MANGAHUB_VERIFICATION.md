# MangaHub verification

The conversion reuses authenticated networking but introduces actual MangaHub
metadata, library, progress, and chapter release workflows.

## Repeatable integration evidence

Run `go test -count=1 -run TestManga -timeout 60s ./internal/tui`.

| Case | Expected | Observed in conversion run |
| --- | --- | --- |
| Catalog | 30 complete records, at least four genres | 30 real titles, 14 genres |
| Empty/unknown search | Empty list / missing-ID error | Both verified |
| Personal library | Account ownership; no duplicate add | Duplicate rejected; another account could not update owner's entry |
| Chapter validation | Reject negative, above-total, unsupported status | All rejected |
| TCP | Other same-account session receives exact committed version/chapter | Received `progress_updated`, chapter 5, version 2; origin and different user received none |
| Visible TUI progress | Second TUI updates without manual refresh | Rendered library changed to chapter 7/version 2 after TCP event |
| UDP release | Two subscribed clients receive new total; non-subscriber receives none | Local Naruto test release to chapter 701 delivered to both subscribers, none to non-subscriber |
| Release authorization | Non-admin and non-increasing total rejected | Both rejected |
| TUI notification | Incoming datagram visible in widget | `New chapter: Naruto` displayed |
| TUI controls | Signup → add title → edit reading progress | Form actions saved reading/chapter 8 |
| Chat | Sender and same room receive; other room receives none | Both general-room clients received; mystery-room client received none |
| SQLite reopen | Progress/version and released chapter remain | Reopened test databases retained chapter 701 and library version 3; reseeding did not overwrite |
| Full process restart | Backend/catalog stop and restart; TUI recovers | Opt-in process test passed; saved chapter 10 survived backend restart, TUI reconnected and displayed chapter 11, catalog release 1200 survived its restart |
| Busy TUI / burst | Last saved progress remains visible | 100 updates during a blocked render converged to chapter 100, version 101 without refresh |
| Catalog stop | Bounded error rather than hanging | HTTP client failed within two-second assertion |

These are automated observations on temporary databases and actual loopback
listeners. They do not claim that the pair has performed the live oral defense.

The actual `cmd/catalog` and `cmd/backend` programs were also run as separate
processes on private test ports. A headless Chrome smoke test passed signup,
30-title browsing, library add, persisted chapter edit, admin release, timestamped
chat, and mobile horizontal-overflow checks. Desktop and mobile screenshots were
visually inspected. The browser is supplementary; TCP/UDP evidence is from TUI.

`go test ./...` and `go vet ./...` passed during conversion. The Windows
`go test -race` attempt was blocked at compilation because `gcc` was not on PATH;
therefore no successful race-detector result is claimed. Run the documented race
command on a supported Go/C toolchain before using it as defense evidence.

## Additional regression coverage

Existing auth, TCP, UDP, WebSocket, storage, and configuration suites remain.
They cover malformed/oversized messages, token checks, unavailable receivers,
unsubscribe/expiry, room isolation, cleanup, and bounded socket operations.

MangaHub feature evidence comes from the `TestManga*` tests above. Transport
regressions exercise reading-progress updates and chapter-release notifications.

## Reliability hardening on 2026-11-27

The original TCP client failed the full-queue regression check; the corrected
implementation passed the same check and cancellation check. See
[symptom, cause, fix and retest](DEFECT_PROGRESS_QUEUE.md).
Library reads now coalesce while the UI is busy, and TCP reconnect triggers
event-driven reconciliation of saved state. The independent-process test is
opt-in: set `MANGAHUB_PROCESS_TEST=1` and run `TestMangaProcessRestart`.

The MangaCatalogService protobuf contract was corrected to use a dedicated
UpdateChaptersResponse. `buf lint` and `buf format --diff --exit-code` are part
of the release verification. This records the actual checked contract, not
an assumed success based only on generated code compiling.

## TCP framing and WebSocket edge cases

The `TestTCPFraming*` cases verify fragmented/coalesced messages on a real TCP
listener, no processing before newline, authentication timeout for an unfinished
frame, malformed JSON recovery, byte limits, and continued service for a healthy
client. Scanner boundary cases verify complete frames, clean EOF and rejection
of an unterminated final frame. The latter failed before the fix: Go's default
line scanner accepts the final token at EOF even without a newline. The server
now uses an explicit newline-required split function with an 8192-byte payload
limit, excluding the delimiter.

WebSocket edge tests cover malformed JSON, wrong body types, missing/blank
bodies, binary messages, oversized UTF-8/ASCII bodies, and exact 2048-byte valid
bodies. Errors stay with the offending connection; peers can still exchange
valid messages. A frame exceeding 4096 bytes closes only its sender with code
1009, removes it from the Hub, and leaves a healthy client usable. Forged room
and username fields do not replace authenticated server-side membership.

The new TCP and WebSocket edge tests passed 20 consecutive runs each. The TCP
scanner was the only production behavior changed by this verification step.

## Concurrent chapter release regression

The release regression failed before the fix: a request setting chapter 2 could
return metadata for chapter 100, because UPDATE and the subsequent SELECT were
separate operations. Chapter releases now use UPDATE RETURNING, so the response
captures that write's metadata. The HTTP handler uses this same gRPC response
for its best-effort UDP notification.

`TestConcurrentChapterReleasesReturnOwnCommittedValue` passed 30 runs with 100
concurrent requests per run. `TestMangaConcurrentReleasesMatchHTTPAndUDP` passed
10 runs through real HTTP/gRPC/UDP listeners, comparing every successful release
with its HTTP response and local UDP event. Concurrent requests overtaken by a
higher chapter are rejected; they must not emit a release alert. UDP arrival
order and guaranteed delivery outside this local test are not promised.

## UDP subscription recovery

The TUI releases its active subscription state when the UDP worker exits with an
initialization, receive, or server-rejection error. The status keeps the failure
reason and invites a manual Subscribe retry. Each subscription has a generation
number: queued alerts, acknowledgements, and completion from an old subscription
cannot overwrite or stop a newer one. Widget/state updates use the tview loop.

The three `TestMangaUDP*` recovery cases passed 20 runs each: initialization
failure followed by a successful retry and visible chapter alert; immediate
unsubscribe/resubscribe with stale completion/acknowledgement rejection; and a
server-rejected token followed by a successful authenticated retry. Invalid target
and token changes are controlled test inputs, not a claim of live configuration
reload in the application. UDP remains best-effort; no replay or automatic
reconnection guarantee is added.

## Chat checks and remaining limitations

Chat/UI hardening checks: `node --test scripts/test-web.cjs` executes the shipped
web script with controlled DOM/network boundaries. It verifies Discussions does
not request the catalog, pending chat drafts survive disconnect, server echoes
clear confirmed drafts, duplicate actions are blocked, and transport failures
update connection status. These are behavioral checks, not a full browser visual
test. Node.js is required by `scripts/verify-demo.ps1` for these checks.

Go chat regressions verify ordered delivery of 20 messages through a one-slot
queue, disconnect reporting, cancellation with a blocked UI, UTF-8 byte limits,
visible TUI draft preservation and mutation guards releasing after failure.
The client no longer silently drops chat events when its UI queue is full.
The server may disconnect clients that cannot keep up; no chat replay is promised.
The web keeps an unconfirmed draft until its own message echo arrives, or warns
on disconnect/timeout. It does not automatically resend uncertain messages.

UDP has no delivery guarantee. Chat history is not persisted. TCP reconnect uses
backoff; changing JWT secrets requires signing in again. Redis is not implemented.
The web interface supplements rather than replaces the required Go tview demo.
