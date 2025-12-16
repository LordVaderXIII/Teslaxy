## 2025-12-11 - Missing DB Indexes
**Learning:** The application sorts clips by timestamp and preloads associations (VideoFiles, Telemetry) on every list request, but the underlying GORM models lacked database indexes. This forces full table scans.
**Action:** Always check `models.go` for `gorm:"index"` on fields used in `Order()` or Foreign Keys, especially in GORM-based backends.

## 2025-12-16 - Frontend List Processing
**Learning:** Large lists of objects with timestamp strings caused blocking UI freezes during sorting and grouping because `new Date(string)` was called repeatedly ($O(N \log N)$).
**Action:** When processing lists, pre-calculate/cache derived values (like timestamps) into a TypedArray ($O(N)$) before sorting or filtering. Also, detect if the list is already effectively sorted to avoid `sort()` entirely.
