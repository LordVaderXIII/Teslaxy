package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"teslaxy/database"
)

const maxLibraryFileIDs = 64

type libraryFileRow struct {
	ClipID    uint      `json:"clip_id"`
	Camera    string    `json:"camera"`
	FilePath  string    `json:"file_path"`
	Timestamp time.Time `json:"timestamp"`
}

type libraryFilesResponse struct {
	Files      []libraryFileRow `json:"files"`
	MissingIDs []uint           `json:"missing_ids"`
}

func parseLibraryFileIDs(c *gin.Context) ([]uint, bool) {
	raw := c.QueryArray("id")
	if len(raw) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return nil, false
	}

	seen := make(map[uint]struct{}, len(raw))
	ids := make([]uint, 0, len(raw))
	for _, s := range raw {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return nil, false
		}
		id := uint(n)
		// Clip primary keys start at 1. Zero is the unset id, not a missing row.
		if id == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) > maxLibraryFileIDs {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too many ids"})
		return nil, false
	}
	return ids, true
}

func loadLiveClipIDs(ids []uint) (map[uint]struct{}, error) {
	var rows []struct{ ID uint }
	err := database.DB.Table("clips").
		Select("clips.id").
		Where("clips.deleted_at IS NULL AND clips.id IN (?)", ids).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint]struct{}, len(rows))
	for _, row := range rows {
		out[row.ID] = struct{}{}
	}
	return out, nil
}

func loadLibraryFiles(ids []uint) ([]libraryFileRow, error) {
	var rows []libraryFileRow
	err := database.DB.Table("video_files").
		Select("video_files.clip_id, video_files.camera, video_files.file_path, video_files.timestamp").
		Joins("INNER JOIN clips ON clips.id = video_files.clip_id").
		Where("clips.deleted_at IS NULL AND video_files.deleted_at IS NULL AND clips.id IN (?)", ids).
		Order("clips.timestamp ASC, video_files.camera ASC, video_files.timestamp ASC, video_files.id ASC").
		Scan(&rows).Error
	return rows, err
}

// dedupeLibraryFiles collapses rows that are the same camera file.
// A merged Saved or Sentry event is several clip ids, and each id can point
// at the same MP4. SQL DISTINCT cannot drop those rows because clip_id differs.
// The first row in the query order (clip time, camera, file time, file id) is kept.
// Both clip ids stay live, so neither is added to missing_ids.
func dedupeLibraryFiles(rows []libraryFileRow) []libraryFileRow {
	if len(rows) < 2 {
		return rows
	}
	seen := make(map[string]struct{}, len(rows))
	out := make([]libraryFileRow, 0, len(rows))
	for _, row := range rows {
		key := libraryFileDedupeKey(row)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	return out
}

func libraryFileDedupeKey(row libraryFileRow) string {
	return canonicalCamera(row.Camera) + "\x00" + row.FilePath
}

func canonicalCamera(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func getLibraryFiles(c *gin.Context) {
	ids, ok := parseLibraryFileIDs(c)
	if !ok {
		return
	}

	live, err := loadLiveClipIDs(ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	missing := make([]uint, 0)
	for _, id := range ids {
		if _, ok := live[id]; !ok {
			missing = append(missing, id)
		}
	}

	files, err := loadLibraryFiles(ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	files = dedupeLibraryFiles(files)
	if files == nil {
		files = []libraryFileRow{}
	}

	c.JSON(http.StatusOK, libraryFilesResponse{
		Files:      files,
		MissingIDs: missing,
	})
}
