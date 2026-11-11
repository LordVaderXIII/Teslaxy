package services

import (
	"fmt"
	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

// writeClipTelemetry never trusts a cached pointer or a foreign back-reference.
// The row and pointer commit together; errors leave both the DB and caller intact.
func (s *ScannerService) writeClipTelemetry(clip *models.Clip, update func(*models.Telemetry)) error {
	if clip == nil || clip.ID == 0 {
		return fmt.Errorf("zero clip ID")
	}
	s.telemetryMu.Lock()
	defer s.telemetryMu.Unlock()
	tx := s.DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer tx.Rollback()
	var current models.Clip
	if err := tx.First(&current, clip.ID).Error; err != nil {
		return err
	}
	var row models.Telemetry
	if current.TelemetryID != 0 {
		err := tx.Where("id = ? AND clip_id = ?", current.TelemetryID, current.ID).First(&row).Error
		if err != nil && !gorm.IsRecordNotFoundError(err) {
			return err
		}
	}
	row.ClipID = current.ID
	update(&row)
	if err := tx.Save(&row).Error; err != nil {
		return err
	}
	result := tx.Model(&models.Clip{}).Where("id = ?", current.ID).UpdateColumn("telemetry_id", row.ID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("clip %d disappeared during telemetry write", current.ID)
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	clip.TelemetryID = row.ID
	return nil
}
