# MangaHub v3.0 implementation checklist

This document supersedes the CyberLab domain-substitution plan. The production
commands now implement MangaHub F01–F06. Redis (F07) is optional and not implemented.

| Requirement | Implementation | Evidence |
| --- | --- | --- |
| Go + Gin, tview, database/sql + SQLite | Existing infrastructure reused; `cmd/tui` starts `NewMangaUI` | Full suite; TUI form test |
| Three programs | `cmd/catalog`, `cmd/backend`, `cmd/tui` | Separate catalog TCP listener in integration tests |
| F01 accounts | bcrypt, JWT, SQLite accounts; reader signup/admin bootstrap | Existing auth tests; MangaHub TUI signup test |
| F02 search/detail | `/api/manga?q=`, `/api/manga/:id` call `MangaCatalog` gRPC | 30 records, empty search, unknown ID, stopped service |
| Dataset | 30 distinct fictional metadata records, six genres; title, author, genres, status, total_chapters, description | `TestMangaConnectedWorkflowAndIsolation` |
| F03 private library | `/api/library` add/list; unique `(user_id,manga_id)` | Duplicate returns error; cross-user isolation |
| Reading progress | PUT `/api/library/:id/progress`; bounded chapter and status; server version increments | Invalid chapter/status; persisted version after reopen |
| TCP assigned job | NDJSON auth + `progress_updated`; other same-user sessions only; after commit | Actual TCP payload and rendered second TUI chapter update |
| F04 releases | Admin-only `/api/admin/manga/:id/release` → gRPC `UpdateChapters` → UDP `new_chapter` | Two subscribers, one non-subscriber, invalid/non-admin release |
| UDP subscription | Existing authenticated subscribe/unsubscribe with expiration, client heartbeat | UDP regression tests and visible TUI notification test |
| F05 chat | Gorilla Hub, room-bound connection identity, one writer, cleanup | MangaHub two-reader room test; existing hub tests |
| F06 boundary | Committed `manga.proto`, generated stubs; catalog SQLite only in catalog process | Real gRPC listener; timeout/status mapping |
| TUI safety | HTTP work off event loop; incoming network events through QueueUpdateDraw | Simulated terminal tests; race detector where supported |
| Persistence | Separate accounts/library and manga databases; only seed an empty catalog | Reopen backend/catalog; chapter and version retained |
| Report and defense | Required 4–5 page English technical PDF; both students explain/demo | Pending final report and student defense |

## Network contracts

- HTTP uses JWT-protected JSON endpoints; user ID comes from the verified token.
- TCP starts with `{"type":"authenticate","token":"...","session_id":"..."}`
  and uses one JSON object per newline. A committed update emits
  `{"type":"progress_updated","manga_id":"star-harbor","chapter":5,"status":"reading","version":2}`.
- UDP uses one JSON per datagram. Releases emit
  `{"type":"new_chapter","manga_id":"star-harbor","title":"Star Harbor","chapter":13}`.
- WebSocket uses the authenticated room URL `/api/ws/rooms/:room`, JSON body
  `{"body":"Hello"}` and server-assigned room/username/time. Fields differ from
  the example consistently; membership cannot be overridden in the payload.
- gRPC uses `SearchManga`, `GetManga`, `UpdateChapters` on `MangaCatalog`.
  Chapter releases must increase the existing total; invalid/missing IDs receive
  gRPC status codes mapped to controlled HTTP errors.

## Migration boundaries

Do not reinterpret solved challenge data as chapter counts. Existing users remain
valid; MangaHub adds `manga` and `user_library` tables with their own meaning.
Historical CyberLab tables are preserved to avoid destructive data loss. The
active server does not register the old challenge API/catalog service.
