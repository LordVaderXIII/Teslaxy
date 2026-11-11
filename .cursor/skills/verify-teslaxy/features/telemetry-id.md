# Telemetry ownership and HTTP rescan

Use only a disposable fixture. Never copy or connect to a live database. From the repository root, with system Go 1.24+, GCC, curl and Python 3 available:

```bash
mkdir -p .git/tmp
export TMPDIR="$PWD/.git/tmp"
export FIX="$TMPDIR/telemetry-id-fixture"
mkdir -p "$FIX/config" "$FIX/footage/SentryClips/event-a" "$FIX/footage/SentryClips/event-b"
```

Create two empty fixture videos (not playable footage) and event metadata. This tests event.json coordinate seeding and row ownership; the Go tests use a mocked SEI extractor to test playback telemetry and file_timing.

```bash
python3 -c 'import json,os,pathlib; root=pathlib.Path(os.environ["FIX"])/"footage"/"SentryClips"; [( (root/name/"2026-11-01_12-00-00-back.mp4").touch(), (root/name/"event.json").write_text(json.dumps({"city":"Fixture", "est_lat":-34.9, "est_lon":138.6}))) for name in ("event-a","event-b")]'
export FOOTAGE_PATH="$FIX/footage" CONFIG_PATH="$FIX/config" PORT=18080 AUTH_ENABLED=false
export RETENTION_ENABLED=false RETENTION_DRY_RUN=true
(cd backend && go build -o "$TMPDIR/teslaxy-telemetry-verify" ./)
# Run from the repo root so /api/version can read CHANGELOG.md.
"$TMPDIR/teslaxy-telemetry-verify" &
SERVER_PID=$!
```

Wait for readiness, then confirm the initial scan has finished (the two fixture clips both have non-zero pointers). A 202 is acceptance, not completion; on a larger fixture poll SQLite until expected clips are indexed and telemetry is rebuilt. A 409 means an existing full scan, including startup, is still running.

```bash
curl -sf http://127.0.0.1:18080/ping
curl -sf http://127.0.0.1:18080/api/version
python3 -c 'import os,sqlite3; db=sqlite3.connect(os.path.join(os.environ["FIX"],"config","teslacam.db")); print("indexed:",db.execute("SELECT COUNT(*) FROM clips WHERE deleted_at IS NULL AND telemetry_id != 0").fetchone()[0])'
curl -sS -w '\nHTTP %{http_code}\n' -X POST http://127.0.0.1:18080/api/scan \
  -H 'X-Requested-With: XMLHttpRequest' \
  -H 'Origin: http://127.0.0.1:18080' -H 'Content-Type: application/json'
```

Expected ping: `{"message":"pong"}`. Expected accepted rescan: HTTP 202, `{"status":"scan started"}`. A simultaneous request gets HTTP 409, `{"error":"scan already running"}`. No queue is created. An unwired scanner returns 503. With AUTH_ENABLED=true, provide a valid bearer token through your existing client without logging it; missing/invalid authentication returns 401. This route uses the same LAN and MutationGuard policies as `/api/retention/run`: direct loopback/private IPv4 only, a custom CSRF header required, and Origin/Referer checked when supplied. The fixture intentionally disables auth and must remain loopback-only.

After completion, this stdlib-only SQLite check must print no shared rows. The exact shared-pointer query is also suitable for sqlite3:

```bash
python3 -c 'import os,sqlite3; db=sqlite3.connect(os.path.join(os.environ["FIX"],"config","teslacam.db")); shared=db.execute("SELECT telemetry_id, COUNT(*) FROM clips WHERE deleted_at IS NULL AND telemetry_id != 0 GROUP BY 1 HAVING COUNT(*) > 1").fetchall(); print("shared telemetry:",shared); assert not shared; counts=db.execute("SELECT COUNT(*), COUNT(DISTINCT telemetry_id), SUM(telemetry_id = 0) FROM clips WHERE deleted_at IS NULL").fetchone(); print("clips, distinct pointers, zero pointers:", counts); assert counts == (2,2,0); mismatches=db.execute("SELECT c.id FROM clips c LEFT JOIN telemetries t ON t.id = c.telemetry_id WHERE c.deleted_at IS NULL AND (t.id IS NULL OR t.clip_id != c.id)").fetchall(); print("owner mismatches:",mismatches); assert not mismatches'
kill "$SERVER_PID"
wait "$SERVER_PID" || true
```

Keep the fixture until evidence is recorded; remove only this fixture directory and binary afterwards. Never kill processes by name.

Targeted regression commands (run separately from backend/, with TMPDIR still exported):

```bash
timeout 280 go test ./services/ -run 'TelemetryID|SharedTelemetry|TelemetryRepair' -count=1 -v
timeout 280 go test ./services/ -run 'Scanner|Telemetry|Backfill|Incident' -count=1
timeout 280 go test ./api/ -run 'Scan|Rescan' -count=1 -v
go build ./...
go vet ./api ./services ./database
```

`TestTelemetryRepairStartupAndScanAll` builds 50 corrupt pointers plus three correct clips, calls real database startup, expects 49 detached links (the owner keeps its link), runs ScanAll, and checks unique non-zero owned rows, unchanged correct rows, unchanged video count, and zero changes on repeat repairs. Repair only clears non-owner pointers; it never deletes rows or footage. Missing source footage cannot reconstruct historical telemetry, and tombstones remain tombstones. No production or kernel/root proof is needed for this backend-only fixture check.
