# Teslaxy — Comprehensive Code Review

**Date:** 2025-05-15  
**Reviewer:** Grok 4.3 (xAI)  
**Scope:** Full codebase (backend Go + frontend React/TypeScript + Docker + domain logic)  
**Version reviewed:** Post 0.1.17 (commit state at time of review)

> **Note from README:** "THIS APP IS IN HEAVY DEVELOPMENT AND MOST FEATURES ARE IN SOME SORT OF BROKEN STATE."

This review is deliberately **brutal, honest, and complete**. It is written as if this were a production system handling real user Tesla footage.

---

## Executive Summary

**Overall Grade: B- (Promising foundation, significant architectural and security risks)**

Teslaxy is an ambitious single-container self-hosted Tesla Dashcam/Sentry viewer with multi-camera synchronized playback, 3D visualization, telemetry extraction from SEI data, real-time scanning, and planned export functionality. The project shows **unusually high security awareness** for its size and stage (custom JWT hardening, SEI DoS protection tests, path traversal tests, constant-time comparisons, auto-generated secrets). However, it suffers from several **foundational architectural problems** that will become increasingly painful as the feature set grows (AI integration, phone support, dashboards, telemetry storage).

### Top 5 Most Critical Issues

| Rank | Issue | Severity | Category |
|------|-------|----------|----------|
| 1 | Custom JWT implementation instead of audited library | **Critical** | Security |
| 2 | GORM v1 (`jinzhu/gorm`) — unmaintained, major migration debt | **Critical** | Maintainability |
| 3 | Authentication disabled by default + no persistent sessions/revocation | **High** | Security |
| 4 | Full recursive `filepath.Walk` + fsnotify on entire footage tree at startup | **High** | Performance / Stability |
| 5 | SQLite as primary store for high-volume telemetry + concurrent writes | **High** | Data Integrity |

---

## 1. Architecture & Design

### Strengths
- Clean separation: `api/`, `models/`, `services/`, `database/`, `proto/`
- Single-binary deployment via Go `embed` of the React build (excellent DX for self-hosting)
- Scanner service with debouncing and `fsnotify` for near real-time updates (well executed)
- Proto definition for SEI metadata is clean and versioned

### Weaknesses

**1.1 The "Merged Clip" Abstraction is Broken by Design**

The core domain concept — "one logical event = multiple 1-minute MP4s + one Telemetry record" — is handled inconsistently:

- Backend stores **individual** 1-minute `Clip` + `VideoFile` rows
- `clipMerge.ts` (frontend) performs complex time-based grouping (65s threshold) to create "super clips"
- Telemetry is only attached to the *first* clip in a group (see `clipMerge.ts:89-91`)
- When you click a merged clip, `handleClipSelect` does a second fetch to `/api/clips/:id` and then **overrides** video_files if the list clip had more files

**Problems:**
- Telemetry for later segments in a long drive is lost or incorrect
- No way to know on the backend which clips belong to the same logical event
- Calendar, Timeline, and Map all operate on this fragile client-side merge
- Export functionality will have to re-implement similar logic

**Recommendation:** Introduce an `Event` or `Session` concept in the database. A background job (or scanner enhancement) should group clips into events and store a canonical `event_id` + aggregated telemetry (or at least first+last telemetry points).

**1.2 No Clear Ownership of "Source of Truth"**

- Some data comes from `event.json` (city, reason, coordinates)
- Some data comes from SEI protobuf in video
- Some data is synthesized on the client (`start_time`, `date_key`)
- The scanner has special cases for `SentryClips`/`SavedClips` vs `RecentClips`

This leads to the many "fixed missing location/telemetry" bugs visible in the changelog.

---

## 2. Security Review (Deep)

This is the **strongest area** of the codebase, but still has fatal flaws.

### What Was Done Well
- Constant-time password comparison (`subtle.ConstantTimeCompare`)
- Rate limiting on `/login` (5 attempts/minute, cleanup goroutine)
- Auto-generation of cryptographically secure `JWT_SECRET` and `ADMIN_PASS` if unset (recent fixes in 0.1.15/0.1.16)
- `SecureLogger` that redacts `token=` query parameters
- Security headers + reasonable CSP (with documented `unsafe-inline` trade-off)
- 1MB request body limit
- Explicit path traversal defense in `serveVideo`
- Excellent `TestExtractSEI_DoS_Protection` and `TestServeVideo_PathTraversal`
- `ExportRequest.Validate()` with duration cap (20 min) and camera allowlist

### Critical Security Issues

**S1: Custom JWT Implementation (Highest Risk)**

```go
// backend/api/auth.go:184
func generateToken(user string) (string, error) {
    header := `{"alg":"HS256","typ":"JWT"}`  // hardcoded string!
    ...
    signatureInput := encodedHeader + "." + encodedPayload
    mac := hmac.New(sha256.New, secretKey)
    ...
}
```

**Problems:**
- No `kid` (key ID)
- No `iss`, `aud`, `iat`
- No support for key rotation
- No library — every future developer must understand this implementation
- `validateToken` does manual base64 + HMAC comparison (correct today, but one mistake = total compromise)

**Recommendation (non-negotiable):** Replace with `github.com/golang-jwt/jwt/v5` (or `golang.org/x/oauth2/jwt` if you want minimal deps). Migrate tokens on next major version.

**S2: Auth is Disabled by Default**

```go
// middleware.go:87
enabled := os.Getenv("AUTH_ENABLED") == "true"
if !enabled {
    c.Next()
    return
}
```

For an application that gives access to **years of personal driving footage, location history, and potentially embarrassing Sentry events**, running unauthenticated by default is unacceptable.

**S3: Token Revocation is Impossible**

Tokens are valid for 24 hours. There is no blacklist, no logout endpoint that invalidates tokens, and no way to force re-auth after password change.

**S4: In-Memory Rate Limiting & Export Queue**

Both `loginAttempts` and `exportQueue` are plain maps protected by mutexes. On container restart (or crash), all rate limit state and in-progress export jobs disappear. An attacker can brute-force after a restart.

**S5: Path Traversal Check is Fragile**

In `serveVideo` (routes.go:107):

```go
if fullPath != cleanFootagePath && !strings.HasPrefix(fullPath, cleanFootagePath+string(os.PathSeparator)) {
    c.JSON(403, ...)
}
```

This is better than many implementations, but still has classic issues on Windows (`\` vs `/`) and with symlinks. The test only covers Unix paths.

---

## 3. Backend — Go Specific

### Positive
- Good use of `sync.Mutex` + debouncing in scanner
- Semaphore pattern for parallel processing of event directories (`ScanAll`)
- `MaxSEINalSize` constant with logging (DoS defense)
- `MaxConcurrentExports = 3` limit

### Serious Problems

**B1: GORM v1 is a Dead End**

```go
require github.com/jinzhu/gorm v1.9.16
```

This library has not been meaningfully updated since ~2020. The modern `gorm.io/gorm` has completely different import paths and some API changes. Every new contributor will be confused.

**B2: SQLite Concurrency & Durability**

- Default SQLite settings (no WAL, no busy_timeout, synchronous=FULL)
- Scanner writes, API reads, and export jobs can all contend
- `AutoMigrate` on every startup in production

**B3: Error Handling is Inconsistent and Often Silent**

Many places do:
```go
n, _ := fp.Read(header)
if n < 4 { break }
```

In `sei_extractor.go`, `iterNals`, `findMdat` — partial reads are common and mostly ignored. On a corrupted MP4 (very common with Tesla drives that were abruptly unplugged), this can produce garbage telemetry or panic later.

**B4: No Structured Logging**

Everything is `fmt.Println` or `log.Printf`. No levels, no structured fields, no request IDs. When you have 50,000 clips, debugging a specific event is painful.

**B5: Scanner Startup Cost**

`ScanAll()` does a full `filepath.Walk` + stat of every `.mp4` + optional `event.json` lookup. Then it adds an `fsnotify` watch to **every subdirectory**. On a large TeslaCam drive this can take 30–90+ seconds and hit OS limits.

**B6: No Graceful Shutdown**

`main.go` has `defer database.CloseDB()` but the scanner watcher, export goroutines, rate limit cleanup, and ffmpeg processes have no shutdown hooks. `Ctrl+C` will leak resources.

---

## 4. Frontend — React/TypeScript

### Positive
- Heavy use of `useCallback` + `useMemo` (Sidebar memoization mentioned in changelog)
- Custom `clipMerge` is surprisingly well-optimized (pre-computed dates, reverse iteration)
- Adapter pattern to make raw `<video>` work with the Player abstraction is clever

### Major Issues

**F1: Three.js + Video Textures Memory Management**

In `Scene3D.tsx`, `CurvedScreen` creates `document.createElement('video')` elements directly. There is no `useEffect` cleanup that calls `video.pause()`, removes `src`, or revokes object URLs when the component unmounts or the clip changes. This is a **classic memory leak** in long-running SPAs.

**F2: No Error Boundaries**

A single video decode error or Three.js context loss will crash the entire app.

**F3: State Management is Approaching Its Limit**

Current pattern (lifting state in `App.tsx` + prop drilling + `fetch` in `useEffect`) will collapse when you add:
- Real-time clip updates via WebSocket / SSE
- AI search
- Multiple selected clips
- User preferences

**F4: Timeline + Scrubber Logic is Extremely Brittle**

The changelog shows repeated fixes for "scrubber jumping", "1-minute clips not grouping", "future clips", "segment transition". This logic lives across `Timeline.tsx`, `Player.tsx`, `clipMerge.ts`, and backend `scanner.go`. It is the highest source of user-visible bugs.

**F5: No Type Safety on API Responses**

The `Clip` interface in `clipMerge.ts` and components is manually maintained and diverges from the Go `models.Clip` struct (different casing, optional fields, `full_data_json` handling).

---

## 5. Domain Logic — Telemetry & Video Processing

### SEI Extractor (`sei_extractor.go`)

This is the most impressive and also the most dangerous piece of code.

**Strengths:**
- Has a dedicated security test that constructs malicious NALs
- `MaxSEINalSize = 1MB` limit
- Emulation prevention byte stripping

**Weaknesses:**
- Extremely fragile binary parsing with almost no bounds checking after the initial NAL size check
- Magic number scanning for `0x42 ... 0x69`
- Comments reference a Python implementation (`dashcam.js/sei_extractor.py`) that is not in the repo — this is a **maintenance nightmare**
- The protobuf message is stored in `FullDataJson` as a string (wasted space + double serialization)

**Recommendation:** The SEI extraction should be moved to a separate, well-tested, memory-safe library (or at least have 10x more unit tests with real Tesla MP4 samples).

---

## 6. Testing & Quality

**Positive:** There are **many** security-focused tests (`*_security_test.go`, `*_perf_test.go`). This is rare.

**Problems:**

- Most tests are **unit tests of isolated functions**, not integration tests of the full scanner → DB → API flow
- No tests for the `clipMerge` algorithm with real-world edge cases (daylight saving, timezone changes, clips that cross midnight, duplicate filenames)
- No contract tests between frontend and backend (the recent "optimized payload" change in 0.1.13 could have broken the frontend silently)
- `TestExtractSEI_DoS_Protection` uses `ioutil` (deprecated)
- Very few table-driven tests

---

## 7. DevOps & Deployment

### Dockerfile
- Multi-stage build is good
- `apk add --no-cache ffmpeg tzdata` — missing `nvidia-vaapi-driver` or proper NVDEC runtime libs for full hardware decode
- `go get` inside the build stage (line 23) is an anti-pattern (should be in go.mod)

### docker-compose.yml
- `AUTH_ENABLED=false` hard-coded
- No healthcheck
- No resource limits (memory, CPU)
- Volume for footage is `:ro` (good), but exports go to `./config/exports` which can fill the host

### Missing
- No `.dockerignore`
- No CI (GitHub Actions) visible
- No backup/restore story for `teslacam.db`
- No log rotation for `server.log` or ffmpeg output

---

## 8. Positive Observations (Credit Where Due)

1. The team (or solo dev) has **fixed real security issues** quickly and publicly in the changelog.
2. Performance optimizations are being made proactively (`Select` only needed columns, payload size reduction).
3. The 3D curved screen implementation with React Three Fiber is non-trivial and mostly working.
4. Rate limiting + secure random generation + constant time compare shows real security thinking.
5. The decision to embed the frontend and ship one container is the correct one for self-hosting.

---

## 9. Prioritized Recommendations

### Must Fix Before v1.0 (Production Use)

1. **Replace custom JWT** with `golang-jwt/jwt/v5`
2. **Migrate from jinzhu/gorm → gorm.io/gorm** (or even better, consider `sqlc` + raw `database/sql` for this use case)
3. **Default `AUTH_ENABLED=true`** and document the generated password clearly on first run
4. **Add persistent job/rate-limit store** (or at minimum use SQLite for the export queue)
5. **Add proper graceful shutdown** (context cancellation, signal handling)
6. **Introduce `Event` model** and stop doing primary grouping on the frontend

### High Priority

7. Add structured logging (`log/slog` or `zap`)
8. Add database migrations instead of `AutoMigrate`
9. Add max inotify watch configuration guidance + fallback to polling for very large libraries
10. Write a real integration test that scans a fixture directory and asserts correct grouping + telemetry
11. Fix video element disposal in `Scene3D.tsx`

### Nice to Have

12. WebSocket or Server-Sent Events for live clip updates (remove polling)
13. Configurable log level
14. Prometheus metrics endpoint (`/metrics`)
15. Proper OpenAPI spec (or at least generated types shared between Go and TS)

---

## 10. Final Verdict

**Teslaxy has a soul.** It solves a real, painful problem (reviewing Tesla footage is miserable) with creativity (3D view, SEI telemetry, real-time scanning). The security mindset is ahead of most solo projects.

However, the current architecture is **accumulating technical debt at an accelerating rate**. The combination of:
- Client-side event grouping
- Custom crypto
- Unmaintained ORM
- Naive full-tree scanning
- In-memory everything

...means that adding the next 10 features from the roadmap (AI, phone, dashboards, multi-user) will be extremely painful and risky.

**Recommendation:** Before adding any major new feature, spend 3–4 weeks on a **"Foundation Sprint"**:
- Replace JWT
- Migrate/ replace GORM
- Introduce proper Event model
- Add structured logging + graceful shutdown
- Write 5 high-value integration tests

Only then should AI integration or mobile support be attempted.

---

**End of Review**

*This document was generated after exhaustive static analysis, reading of security tests, domain logic, state management, build pipeline, and cross-referencing against the changelog and known recent bug fixes.*

*Location of this report: `docs/REVIEW.md`*

---

## Post-Review Update (2025-05-15)

### Database Migrations — Now Explicitly Automatic

As part of addressing architectural issues 1.1 and 1.2 (introducing `SourceDir` on the `Clip` model), the following was done:

- Confirmed that `backend/database/db.go` already calls `DB.AutoMigrate(...)` on every startup.
- This **is** the automatic migration mechanism for the project today.
- Added a large, clear explanatory comment block inside `InitDB()` documenting:
  - How `AutoMigrate` handles new columns + indexes (e.g. the new `source_dir` column + index)
  - The current strategy (acceptable during heavy development / pre-v1.0)
  - What developers must do when adding future model fields (just add the field + gorm tags)
  - The known long-term plan to move to proper migration tooling

**Result**: Adding the `SourceDir` field (and any future additive schema changes) is now **fully automatic**. No manual SQL or migration files are required. The app will create the column and index on the next restart.

This does **not** change the recommendation in the main review (we still believe proper migrations + moving off jinzhu/gorm should happen before v1.0), but it makes the *current* reality clear, documented, and reliable for ongoing work.