package database

import (
	"reflect"
	"testing"

	"github.com/jinzhu/gorm"
	"teslaxy/models"
)

func TestTelemetryRepairMissingOwnerAndTombstones(t *testing.T) {
	for _, ownerState := range []string{"missing", "deleted telemetry", "deleted clip"} {
		t.Run(ownerState, func(t *testing.T) {
			db, err := gorm.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.DB().SetMaxOpenConns(1)
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			check(db.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{}).Error)
			owner, other := models.Clip{}, models.Clip{}
			check(db.Create(&owner).Error)
			check(db.Create(&other).Error)
			row := models.Telemetry{ClipID: owner.ID, FullDataJson: "preserved"}
			if ownerState == "missing" {
				row.ClipID = 99999
			}
			check(db.Create(&row).Error)
			check(db.Model(&owner).UpdateColumn("telemetry_id", row.ID).Error)
			check(db.Model(&other).UpdateColumn("telemetry_id", row.ID).Error)
			if ownerState == "deleted telemetry" {
				check(db.Delete(&row).Error)
			}
			if ownerState == "deleted clip" {
				check(db.Delete(&owner).Error)
			}
			var before models.Telemetry
			check(db.Unscoped().First(&before, row.ID).Error)
			wantCleared := int64(2)
			if ownerState == "deleted clip" {
				wantCleared = 1 // Keep the rightful tombstoned owner's link.
			}
			n, err := RepairSharedTelemetry(db)
			check(err)
			if n != wantCleared {
				t.Fatalf("cleared %d links, want %d", n, wantCleared)
			}
			n, err = RepairSharedTelemetry(db)
			check(err)
			if n != 0 {
				t.Fatalf("repeat repair changed %d links", n)
			}
			check(db.Unscoped().First(&owner, owner.ID).Error)
			check(db.First(&other, other.ID).Error)
			if other.TelemetryID != 0 || (ownerState == "deleted clip" && (owner.TelemetryID != row.ID || owner.DeletedAt == nil)) || (ownerState != "deleted clip" && owner.TelemetryID != 0) {
				t.Fatal("repair left invalid pointers or changed tombstone state")
			}
			var preserved models.Telemetry
			check(db.Unscoped().First(&preserved, row.ID).Error)
			if !reflect.DeepEqual(preserved, before) {
				t.Fatal("repair changed telemetry data or tombstone state")
			}
		})
	}
}
