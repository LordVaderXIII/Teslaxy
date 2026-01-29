package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/database"
)

func TestErrorLeak_GetClips(t *testing.T) {
	// Save original global DB state to avoid pollution
	originalDB := database.DB
	defer func() {
		database.DB = originalDB
	}()

	// Setup DB for this test
	dbPath := "test_leak_clips.db"
	var err error
	database.DB, err = gorm.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	defer func() {
		// Close the test DB
		database.DB.Close()
		os.Remove(dbPath)
	}()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/clips", getClips)

	// Close DB to force error
	database.DB.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/clips", nil)
	r.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Errorf("Expected 500, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	errMsg := resp["error"]
	t.Logf("Response error: %s", errMsg)

	// Check for leak
	if errMsg == "sql: database is closed" {
		t.Error("Vulnerability DETECTED: SQL error leaked in getClips")
	} else if errMsg == "Internal Server Error" {
		t.Log("Verified: Secure error message returned")
	} else {
		t.Errorf("Unexpected error message: %s", errMsg)
	}
}

func TestErrorLeak_Export(t *testing.T) {
	// Save original global DB state
	originalDB := database.DB
	defer func() {
		database.DB = originalDB
	}()

	// Setup DB
	dbPath := "test_leak_export.db"
	var err error
	database.DB, err = gorm.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	defer func() {
		database.DB.Close()
		os.Remove(dbPath)
	}()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/export", createExportJob)

	// Close DB to force error
	database.DB.Close()

	w := httptest.NewRecorder()
	// Valid request body to pass validation
	body := []byte(`{"clip_id": 1, "cameras": ["front"], "start_time": 0, "duration": 10}`)
	req, _ := http.NewRequest("POST", "/api/export", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// We expect 500 because DB lookup fails
	if w.Code != 500 {
		t.Errorf("Expected 500, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		// Just log body if parse fails
		log.Printf("Body: %s", w.Body.String())
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	errMsg := resp["error"]
	t.Logf("Response error: %s", errMsg)

	// Check for leak
	if errMsg == "sql: database is closed" {
		t.Error("Vulnerability DETECTED: SQL error leaked in createExportJob")
	} else if errMsg == "Internal Server Error" {
		t.Log("Verified: Secure error message returned")
	} else {
		t.Errorf("Unexpected error message: %s", errMsg)
	}
}
