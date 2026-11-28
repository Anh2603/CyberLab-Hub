# Defect evidence: dropped TCP progress at a busy terminal

## Symptom and cause

The TCP reader parsed progress_updated correctly, then used a nonblocking channel
send with a default branch. If the TUI event queue was full, the event was silently
discarded. The database could hold a newer chapter while the second TUI displayed
an older one until another event or manual navigation caused a reload.

## Reproduction observed on 2026-11-27

`TestProgressSyncBackpressureAndCancellation` fills a one-slot event channel with
a sentinel, sends 20 valid newline-framed progress updates over a real TCP socket,
and delays the UI consumer. Running the test against the original implementation
failed with:

```text
TCP reader exited while UI queue was full; updates were dropped
```

This is an intentionally busy consumer, not injected packet loss. The failure is
at the application queue after TCP has delivered the stream.

## Fix

- Progress delivery now waits for queue capacity or context cancellation instead
  of discarding state updates. UI rendering stays on the tview event loop.
- Library refreshes coalesce while one request is active; superseded snapshots
  are not painted over newer state. No periodic HTTP polling is added.
- MangaHub's TCP worker reconnects with bounded exponential backoff after failure.
  A successful handshake triggers a library reload, recovering writes missed
  during disconnection. The same JWT secret is needed after backend restart.

## Retest observations

- The same 20-event regression test passed; every chapter/version arrived in order.
- A separate cancellation test passed with a full UI queue (no blocked shutdown).
- `TestMangaTerminalBurstConvergesAfterBusyUI` blocked terminal rendering during
  100 committed updates. After release, its rendered library showed chapter 100,
  version 101, without manual refresh.
- `TestMangaProcessRestart` stopped actual backend/catalog binaries. Saved progress
  survived, the TUI reconnected and showed chapter 11, and the released catalog
  total survived its own restart. Catalog downtime produced a bounded HTTP error.

Re-run commands are in DEMO.md. This evidence is about the tested cases; it does
not claim zero defects or guaranteed delivery of UDP notifications.
