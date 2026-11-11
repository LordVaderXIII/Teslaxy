package database

import (
	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

// RepairSharedTelemetry detaches invalid shared pointers, including tombstones.
// It preserves the rightful owner's link and every telemetry/video/clip row.
// The next scan rebuilds detached telemetry from each clip's own source.
func RepairSharedTelemetry(db *gorm.DB) (int64, error) {
	tx := db.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	defer tx.Rollback()
	result := tx.Unscoped().Model(&models.Clip{}).
		Where(`telemetry_id != 0 AND telemetry_id IN (
   SELECT telemetry_id FROM clips WHERE telemetry_id != 0
   GROUP BY telemetry_id HAVING COUNT(*) > 1
  ) AND NOT EXISTS (
   SELECT 1 FROM telemetries WHERE telemetries.id = clips.telemetry_id
   AND telemetries.clip_id = clips.id AND telemetries.deleted_at IS NULL
  )`).UpdateColumn("telemetry_id", 0)
	if result.Error != nil {
		return 0, result.Error
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return result.RowsAffected, nil
}
