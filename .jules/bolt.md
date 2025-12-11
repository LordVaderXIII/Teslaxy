## 2025-12-11 - Missing DB Indexes
**Learning:** The application sorts clips by timestamp and preloads associations (VideoFiles, Telemetry) on every list request, but the underlying GORM models lacked database indexes. This forces full table scans.
**Action:** Always check `models.go` for `gorm:"index"` on fields used in `Order()` or Foreign Keys, especially in GORM-based backends.
