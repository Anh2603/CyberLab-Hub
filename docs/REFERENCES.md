# MangaHub references

- IT096IIU MangaHub Project Student Specification, version 3.0: assignment supplied
  by the student. F01–F06 define the required runtime workflows.
- [Gin](https://gin-gonic.com/en/docs/): backend HTTP routing/middleware library.
- [tview](https://github.com/rivo/tview): required Go terminal UI library.
- [Gorilla WebSocket](https://github.com/gorilla/websocket): WebSocket transport.
- [gRPC Go](https://grpc.io/docs/languages/go/) and
  [Protocol Buffers](https://protobuf.dev/): catalog service/stub generation.
- [database/sql](https://pkg.go.dev/database/sql) and
  [modernc SQLite](https://pkg.go.dev/modernc.org/sqlite): persisted backend/catalog.
- [Manga metadata sources](MANGA_SOURCES.md): per-title sources and snapshot notes.

Dependency versions/checksums are in go.mod/go.sum. Generated stubs come from this
repository's own .proto files. Earlier CyberLab design inspiration is preserved
in Git history; it is not the active MangaHub specification.
