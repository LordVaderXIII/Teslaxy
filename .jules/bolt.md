## 2025-12-11 - Missing DB Indexes
**Learning:** The application sorts clips by timestamp and preloads associations (VideoFiles, Telemetry) on every list request, but the underlying GORM models lacked database indexes. This forces full table scans.
**Action:** Always check `models.go` for `gorm:"index"` on fields used in `Order()` or Foreign Keys, especially in GORM-based backends.

## 2025-10-26 - React Canvas Re-renders
**Learning:** High-frequency state updates (10Hz playback time) in the parent `Player` component triggered full re-renders of the heavy `Scene3D` WebGL component because it wasn't memoized.
**Action:** Wrap heavy visualization components (especially `react-three-fiber`/`canvas`) in `React.memo` when the parent component has a fast update loop (like a playback timer).
