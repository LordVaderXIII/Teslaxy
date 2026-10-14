# Feature: scanner correctness (H1, H2, M1, M2, L3)

Synthetic fixtures only (`/tmp`, `ffmpeg -f lavfi -i testsrc`). Never real footage.

## Automated

```bash
cd backend
go vet ./...
go test -race ./services -count=1 \
  -run 'TestScanAll_UnreadableSubdirContinues|TestRecentFallback_DoesNotAttachToSentry|TestReconcileMissing_SoftDeletesGoneFootage|TestHandleFileCreate_ScansPrepopulatedDir|TestScanDir_ConcurrentNoDuplicateClips|TestReconcileMissing_RefusesMissingOrEmptyRoot|TestScanner_TakesMediaLock|TestRecentRescan_DeletedFirstMinuteNotDuplicated|TestHandleFileCreate_ScansNestedEvents|TestScanAll_ConcurrentRecentIsIdempotent|TestReconcileMissing_ClipCleanupScopedToRoot|TestReconcileMissing_AbortsWhenRootVanishesMidDelete|TestReconcileMissing_AbortsWhenRootSwappedEmptyMidDelete|TestReconcileMissing_EmptiesLibraryAfterTwoStableEmptyScans|TestReconcileMissing_DeletesOnlyTopLevelVideoAfterConfirm|TestReconcileMissing_ForceEmptyPurgeSkipsConfirm'
go test -race ./... -count=1
```

| Finding | Test |
|---|---|
| H1 | `TestScanAll_UnreadableSubdirContinues` |
| H2 | `TestRecentFallback_DoesNotAttachToSentry` |
| M1 | `TestReconcileMissing_SoftDeletesGoneFootage` |
| M2 | `TestHandleFileCreate_ScansPrepopulatedDir` |
| L3 | `TestScanDir_ConcurrentNoDuplicateClips` |
| P1 | `TestReconcileMissing_RefusesMissingOrEmptyRoot` |
| P2 lock | `TestScanner_TakesMediaLock` |
| P2 recent dup | `TestRecentRescan_DeletedFirstMinuteNotDuplicated` |
| P2 nested walk | `TestHandleFileCreate_ScansNestedEvents` |
| P3 recent race | `TestScanAll_ConcurrentRecentIsIdempotent` |
| P3 root scope | `TestReconcileMissing_ClipCleanupScopedToRoot` |
| Round-3 P1 mid-pass vanish | `TestReconcileMissing_AbortsWhenRootVanishesMidDelete` |
| Round-3 P1 mid-pass empty swap | `TestReconcileMissing_AbortsWhenRootSwappedEmptyMidDelete` |
| Round-3 P2 empty library | `TestReconcileMissing_EmptiesLibraryAfterTwoStableEmptyScans` |
| Round-3 P2 top-level video | `TestReconcileMissing_DeletesOnlyTopLevelVideoAfterConfirm` |
| Round-3 P2 force purge | `TestReconcileMissing_ForceEmptyPurgeSkipsConfirm` |

## Reconcile root safety (round 3)

`ReconcileMissing` captures the footage root's device+inode at start and re-checks that identity before every soft-delete batch (`ErrReconcileRootChanged` if it vanishes or is swapped).

An empty-but-present root with live DB rows is refused on the first sighting (`ErrReconcileRefused`). A second consecutive scan with the **same** device/inode soft-deletes the dead rows (intentional empty library). An empty directory whose inode differs from the last non-empty sighting is refused (drive drop-out / placeholder). `ReconcileOpts.ForceEmptyPurge` skips the two-scan confirm while still requiring the root to exist.

## Manual fixture (run the code)

```bash
FIX=/tmp/teslaxy-scanner-$$
mkdir -p "$FIX/footage/RecentClips" "$FIX/footage/SentryClips" "$FIX/config" "$FIX/staged"
ffmpeg -y -f lavfi -i testsrc=size=320x240:rate=30 -t 1 -c:v libx264 -pix_fmt yuv420p \
  "$FIX/footage/RecentClips/2024-03-10_14-00-00-front.mp4"

# H1: unreadable sibling must not empty the library
mkdir -p "$FIX/footage/RecentClips/locked"
chmod 000 "$FIX/footage/RecentClips/locked"

export FOOTAGE_PATH="$FIX/footage" CONFIG_PATH="$FIX/config" PORT=18081 AUTH_ENABLED=false
cd backend && go build -o /tmp/teslaxy-scanner ./
/tmp/teslaxy-scanner >"$FIX/server.log" 2>&1 & echo $! >"$FIX/server.pid"
for i in $(seq 1 40); do curl -sf http://127.0.0.1:18081/ping | grep -q pong && break; sleep 0.25; done

curl -sf http://127.0.0.1:18081/api/library | tee /opt/cursor/artifacts/verify-scanner-library.json
# Expect at least the Recent seed clip; log should say "Skipping unreadable path" not abort.

# M2: mv a pre-populated Sentry event dir while the server runs
mkdir -p "$FIX/staged/2024-03-15_12-00-00"
ffmpeg -y -f lavfi -i testsrc=size=320x240:rate=30 -t 1 -c:v libx264 -pix_fmt yuv420p \
  "$FIX/staged/2024-03-15_12-00-00/2024-03-15_12-00-00-front.mp4"
echo '{"timestamp":"2024-03-15T12:00:10","city":"Fixture","reason":"test"}' \
  >"$FIX/staged/2024-03-15_12-00-00/event.json"
mv "$FIX/staged/2024-03-15_12-00-00" "$FIX/footage/SentryClips/"
sleep 3
curl -sf http://127.0.0.1:18081/api/library | tee /opt/cursor/artifacts/verify-scanner-after-mv.json
# Expect a Sentry row for 2024-03-15_12-00-00

# M1: delete footage; restart (ScanAll → ReconcileMissing) drops ghosts
rm -rf "$FIX/footage/SentryClips/2024-03-15_12-00-00"
kill "$(cat "$FIX/server.pid")"; sleep 1
/tmp/teslaxy-scanner >"$FIX/server2.log" 2>&1 & echo $! >"$FIX/server.pid"
for i in $(seq 1 40); do curl -sf http://127.0.0.1:18081/ping | grep -q pong && break; sleep 0.25; done
curl -sf http://127.0.0.1:18081/api/library | tee /opt/cursor/artifacts/verify-scanner-after-rm.json
# Sentry event must be gone from /api/library

chmod 755 "$FIX/footage/RecentClips/locked" 2>/dev/null || true
kill "$(cat "$FIX/server.pid")"; rm -rf "$FIX"
```

## Shared API for #238

`services.ReconcileMissing(db, root, opts)` soft-deletes missing `VideoFile` rows and empty `Clip`s under `root`, holding `mediaOpMu` write lock (`media_coord.go`). Retention PR #238 should call this instead of a private post-delete cleanup. Optional `opts.ForceEmptyPurge` clears an intentionally emptied root in one pass; otherwise two consecutive empty scans with a stable root identity are required.
