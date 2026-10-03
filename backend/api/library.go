package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"teslaxy/database"
	"teslaxy/services"
)

// LibraryEvent is the compact library row. Grouping is scanner-owned: this
// endpoint does not split or merge clips, and it does not paginate raw rows.
type LibraryEvent struct {
	ID                 uint       `json:"id"`
	Timestamp          time.Time  `json:"timestamp"`
	EventTimestamp     *time.Time `json:"event_timestamp"`
	Event              string     `json:"event"`
	City               string     `json:"city"`
	Reason             string     `json:"reason"`
	SourceDir          string     `json:"source_dir,omitempty"`
	Latitude           *float64   `json:"latitude,omitempty"`
	Longitude          *float64   `json:"longitude,omitempty"`
	VideoFileCount     int        `json:"video_file_count"`
	PreviewCamera      string     `json:"preview_camera"`
	PreviewPath        string     `json:"preview_path"`
	PreviewTimestamp   *time.Time `json:"preview_timestamp,omitempty"`
	PreviewSeekSeconds float64    `json:"preview_seek_seconds"`
}

type libraryFacetDate struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type libraryFacetEvent struct {
	Event string `json:"event"`
	Count int    `json:"count"`
}

type libraryFacetReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type libraryFacets struct {
	Dates   []libraryFacetDate   `json:"dates"`
	Events  []libraryFacetEvent  `json:"events"`
	Reasons []libraryFacetReason `json:"reasons"`
}

type libraryResponse struct {
	Events []LibraryEvent `json:"events"`
	Facets libraryFacets  `json:"facets"`
}

type libraryClipRow struct {
	ID             uint
	Timestamp      time.Time
	EventTimestamp *time.Time
	Event          string
	City           string
	Reason         string
	SourceDir      string
	Latitude       float64
	Longitude      float64
}

type libraryPreviewRow struct {
	ClipID    uint
	Camera    string
	FilePath  string
	Timestamp time.Time
}

type libraryCountRow struct {
	ClipID uint
	Count  int
}

var allowedLibraryEvents = map[string]bool{
	"Sentry": true,
	"Saved":  true,
	"Recent": true,
}

func parseLibraryQuery(c *gin.Context) (date *time.Time, events []string, reasons []string, ok bool) {
	if raw := c.Query("date"); raw != "" {
		t, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
		if err != nil || t.Format("2006-01-02") != raw {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
			return nil, nil, nil, false
		}
		date = &t
	}

	for _, ev := range c.QueryArray("event") {
		if !allowedLibraryEvents[ev] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event filter, expected Sentry, Saved, or Recent"})
			return nil, nil, nil, false
		}
		events = append(events, ev)
	}

	for _, reason := range c.QueryArray("reason") {
		if reason == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "reason filter must be non-empty"})
			return nil, nil, nil, false
		}
		reasons = append(reasons, reason)
	}

	return date, events, reasons, true
}

func applyEventReasonFilters(db *gorm.DB, events, reasons []string) *gorm.DB {
	if len(events) > 0 {
		db = db.Where("clips.event IN (?)", events)
	}
	if len(reasons) > 0 {
		db = db.Where("clips.reason IN (?)", reasons)
	}
	return db
}

func applyDateFilter(db *gorm.DB, date *time.Time) *gorm.DB {
	if date == nil {
		return db
	}
	start := *date
	end := start.Add(24 * time.Hour)
	return db.Where("clips.timestamp >= ? AND clips.timestamp < ?", start, end)
}

func applyLibraryClipFilters(db *gorm.DB, date *time.Time, events, reasons []string) *gorm.DB {
	db = db.Where("clips.deleted_at IS NULL")
	db = applyEventReasonFilters(db, events, reasons)
	return applyDateFilter(db, date)
}

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

func libraryFilterSQL(date *time.Time, events, reasons []string) (string, []interface{}) {
	parts := []string{"clips.deleted_at IS NULL"}
	var args []interface{}
	if date != nil {
		parts = append(parts, "clips.timestamp >= ? AND clips.timestamp < ?")
		args = append(args, *date, date.Add(24*time.Hour))
	}
	if len(events) > 0 {
		parts = append(parts, "clips.event IN ("+sqlPlaceholders(len(events))+")")
		for _, ev := range events {
			args = append(args, ev)
		}
	}
	if len(reasons) > 0 {
		parts = append(parts, "clips.reason IN ("+sqlPlaceholders(len(reasons))+")")
		for _, reason := range reasons {
			args = append(args, reason)
		}
	}
	return strings.Join(parts, " AND "), args
}

func loadLibraryClips(date *time.Time, events, reasons []string) ([]libraryClipRow, error) {
	var rows []libraryClipRow
	q := applyLibraryClipFilters(database.DB.Table("clips"), date, events, reasons)
	err := q.Select(`clips.id, clips.timestamp, clips.event_timestamp, clips.event, clips.city, clips.reason, clips.source_dir,
		telemetries.latitude, telemetries.longitude`).
		Joins("LEFT JOIN telemetries ON telemetries.id = clips.telemetry_id AND telemetries.deleted_at IS NULL").
		Order("clips.timestamp DESC").
		Scan(&rows).Error
	return rows, err
}

func loadVideoFileCounts(date *time.Time, events, reasons []string) (map[uint]int, error) {
	var rows []libraryCountRow
	q := applyLibraryClipFilters(database.DB.Table("video_files"), date, events, reasons)
	err := q.Select("video_files.clip_id, COUNT(*) as count").
		Joins("INNER JOIN clips ON clips.id = video_files.clip_id AND clips.deleted_at IS NULL").
		Where("video_files.deleted_at IS NULL").
		Group("video_files.clip_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint]int, len(rows))
	for _, row := range rows {
		out[row.ClipID] = row.Count
	}
	return out, nil
}

// loadLibraryPreviews picks one video file per filtered clip using Sidebar rules:
//  1. If event_timestamp is set: latest Front with timestamp <= event_timestamp (tie-break greater id)
//  2. Else / none: earliest Front (timestamp ASC, id ASC)
//  3. If no Front: earliest any-camera file (timestamp ASC, id ASC)
//
// Uses SQLite window functions (probed at development time against this
// process's go-sqlite3). Joins the filtered clips set; never expands clip IDs
// into an IN (?) bind list.
func loadLibraryPreviews(date *time.Time, events, reasons []string) (map[uint]libraryPreviewRow, error) {
	whereSQL, args := libraryFilterSQL(date, events, reasons)
	query := `
SELECT clip_id, camera, file_path, timestamp FROM (
	SELECT vf.clip_id, vf.camera, vf.file_path, vf.timestamp,
		ROW_NUMBER() OVER (
			PARTITION BY vf.clip_id
			ORDER BY
				CASE
					WHEN vf.camera = 'Front' AND clips.event_timestamp IS NOT NULL AND vf.timestamp <= clips.event_timestamp THEN 0
					WHEN vf.camera = 'Front' THEN 1
					ELSE 2
				END ASC,
				CASE
					WHEN vf.camera = 'Front' AND clips.event_timestamp IS NOT NULL AND vf.timestamp <= clips.event_timestamp THEN vf.timestamp
				END DESC,
				CASE
					WHEN vf.camera = 'Front' AND clips.event_timestamp IS NOT NULL AND vf.timestamp <= clips.event_timestamp THEN vf.id
				END DESC,
				CASE
					WHEN NOT (vf.camera = 'Front' AND clips.event_timestamp IS NOT NULL AND vf.timestamp <= clips.event_timestamp) THEN vf.timestamp
				END ASC,
				CASE
					WHEN NOT (vf.camera = 'Front' AND clips.event_timestamp IS NOT NULL AND vf.timestamp <= clips.event_timestamp) THEN vf.id
				END ASC
		) AS rn
	FROM video_files vf
	INNER JOIN clips ON clips.id = vf.clip_id
	WHERE vf.deleted_at IS NULL AND ` + whereSQL + `
) ranked WHERE rn = 1`

	var rows []libraryPreviewRow
	if err := database.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]libraryPreviewRow, len(rows))
	for _, row := range rows {
		out[row.ClipID] = row
	}
	return out, nil
}

func loadDateFacets(events, reasons []string) ([]libraryFacetDate, error) {
	var rows []libraryFacetDate
	q := applyLibraryClipFilters(database.DB.Table("clips"), nil, events, reasons)
	err := q.Select("strftime('%Y-%m-%d', clips.timestamp) as date, count(*) as count").
		Group("strftime('%Y-%m-%d', clips.timestamp)").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []libraryFacetDate{}
	}
	return rows, nil
}

func loadEventFacets(date *time.Time, reasons []string) ([]libraryFacetEvent, error) {
	var rows []libraryFacetEvent
	q := applyLibraryClipFilters(database.DB.Table("clips"), date, nil, reasons)
	err := q.Select("clips.event as event, count(*) as count").
		Group("clips.event").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []libraryFacetEvent{}
	}
	return rows, nil
}

func loadReasonFacets(date *time.Time, events []string) ([]libraryFacetReason, error) {
	var rows []libraryFacetReason
	q := applyLibraryClipFilters(database.DB.Table("clips"), date, events, nil).
		Where("clips.reason != ?", "")
	err := q.Select("clips.reason as reason, count(*) as count").
		Group("clips.reason").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []libraryFacetReason{}
	}
	return rows, nil
}

func previewSeekSeconds(eventTS *time.Time, previewTS time.Time) float64 {
	if eventTS == nil || previewTS.IsZero() {
		return 0
	}
	diff := eventTS.Sub(previewTS).Seconds()
	if diff >= 0 && diff < 600 {
		return diff
	}
	return 0
}

func toLibraryEvent(clip libraryClipRow, fileCount int, preview libraryPreviewRow, hasPreview bool) LibraryEvent {
	ev := LibraryEvent{
		ID:             clip.ID,
		Timestamp:      clip.Timestamp,
		EventTimestamp: clip.EventTimestamp,
		Event:          clip.Event,
		City:           clip.City,
		Reason:         clip.Reason,
		SourceDir:      clip.SourceDir,
		VideoFileCount: fileCount,
	}
	if services.ValidGeographicPoint(clip.Latitude, clip.Longitude) {
		lat := clip.Latitude
		lon := clip.Longitude
		ev.Latitude = &lat
		ev.Longitude = &lon
	}
	if hasPreview {
		ts := preview.Timestamp
		ev.PreviewCamera = preview.Camera
		ev.PreviewPath = preview.FilePath
		ev.PreviewTimestamp = &ts
		ev.PreviewSeekSeconds = previewSeekSeconds(clip.EventTimestamp, preview.Timestamp)
	}
	return ev
}

// getLibrary returns compact logical events plus facets.
// Date filter is the UTC calendar day of Clip.Timestamp.
// Facet behaviour (pinned by tests):
//   - dates: AFTER event/reason filters, BEFORE the date filter (calendar still shows other days)
//   - events: AFTER date/reason filters, BEFORE the event filter
//   - reasons: AFTER date/event filters, BEFORE the reason filter
//
// page/limit query params are ignored; this endpoint never pages raw clip rows.
func getLibrary(c *gin.Context) {
	date, events, reasons, ok := parseLibraryQuery(c)
	if !ok {
		return
	}

	dateFacets, err := loadDateFacets(events, reasons)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	eventFacets, err := loadEventFacets(date, reasons)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	reasonFacets, err := loadReasonFacets(date, events)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	clips, err := loadLibraryClips(date, events, reasons)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	counts, err := loadVideoFileCounts(date, events, reasons)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	previews, err := loadLibraryPreviews(date, events, reasons)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	out := make([]LibraryEvent, 0, len(clips))
	for _, clip := range clips {
		preview, hasPreview := previews[clip.ID]
		out = append(out, toLibraryEvent(clip, counts[clip.ID], preview, hasPreview))
	}

	c.JSON(http.StatusOK, libraryResponse{
		Events: out,
		Facets: libraryFacets{
			Dates:   dateFacets,
			Events:  eventFacets,
			Reasons: reasonFacets,
		},
	})
}
