## 2025-12-11 - Missing DB Indexes
**Learning:** The application sorts clips by timestamp and preloads associations (VideoFiles, Telemetry) on every list request, but the underlying GORM models lacked database indexes. This forces full table scans.
**Action:** Always check `models.go` for `gorm:"index"` on fields used in `Order()` or Foreign Keys, especially in GORM-based backends.

## 2025-12-18 - Oversized Thumbnails
**Learning:** The backend thumbnail service defaulted to 480px width, but the Sidebar list view only renders 48px icons. This resulted in ~90% wasted bandwidth per image.
**Action:** Always verify that asset generation endpoints support dynamic sizing (e.g., query params) to match frontend UI requirements.
