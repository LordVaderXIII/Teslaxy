package api

import (
	"fmt"
	"testing"

	"teslaxy/database"
	"teslaxy/models"
)

func TestProtectionBatchAllMembers(t *testing.T) {
	r, _, _, _ := setupRetentionAPI(t, false)
	second := models.Clip{Event: "Recent"}
	if err := database.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	for _, desired := range []bool{true, false} {
		body := fmt.Sprintf(`{"protected":%t,"ids":[%d,%d]}`, desired, second.ID, second.ID)
		w := retentionRequest(r, "POST", "/api/clips/1/protect", body, "127.0.0.1:1234", "http://localhost", true, "")
		if w.Code != 200 {
			t.Fatalf("batch: %d", w.Code)
		}
		for _, id := range []uint{1, second.ID} {
			var clip models.Clip
			if err := database.DB.First(&clip, id).Error; err != nil || clip.Protected != desired {
				t.Fatalf("member %d not updated atomically: %v", id, err)
			}
		}
	}
}

func TestProtectionBatchRollbackOnMidBatchFailure(t *testing.T) {
	for _, desired := range []bool{true, false} {
		t.Run(fmt.Sprint(desired), func(t *testing.T) {
			r, _, _, _ := setupRetentionAPI(t, false)
			second := models.Clip{Event: "Recent", Protected: !desired}
			if err := database.DB.Create(&second).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.DB.Model(&models.Clip{}).Where("id = ?", 1).Update("protected", !desired).Error; err != nil {
				t.Fatal(err)
			}
			// A real SQLite failure on the second update, after the first has succeeded.
			trigger := fmt.Sprintf(`CREATE TRIGGER fail_protect BEFORE UPDATE OF protected ON clips WHEN OLD.id = %d BEGIN SELECT RAISE(ABORT, 'injected batch failure'); END`, second.ID)
			if err := database.DB.Exec(trigger).Error; err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"protected":%t,"ids":[1,%d]}`, desired, second.ID)
			w := retentionRequest(r, "POST", "/api/clips/1/protect", body, "127.0.0.1:1234", "http://localhost", true, "")
			if w.Code != 500 {
				t.Errorf("want mid-batch failure, got %d", w.Code)
			}
			for _, id := range []uint{1, second.ID} {
				var clip models.Clip
				if err := database.DB.First(&clip, id).Error; err != nil || clip.Protected != !desired {
					t.Errorf("member %d changed after failed batch: %v", id, err)
				}
			}
		})
	}
}

func TestProtectionBatchRejectsInvalidMembers(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"protected":true,"ids":[]}`, 400},
		{`{"protected":true,"ids":null}`, 400},
		{`{"protected":true,"ids":[0]}`, 400},
		{`{"protected":true,"ids":[-1]}`, 400},
		{`{"protected":true,"ids":["1"]}`, 400},
		{`{"protected":true,"ids":[1,99999]}`, 404},
	} {
		t.Run(tc.body, func(t *testing.T) {
			r, _, _, _ := setupRetentionAPI(t, false)
			w := retentionRequest(r, "POST", "/api/clips/1/protect", tc.body, "127.0.0.1:1234", "http://localhost", true, "")
			if w.Code != tc.want {
				t.Errorf("want %d got %d", tc.want, w.Code)
			}
			var clip models.Clip
			if err := database.DB.First(&clip, 1).Error; err != nil || clip.Protected {
				t.Fatalf("invalid batch changed anchor: %v", err)
			}
		})
	}
}
