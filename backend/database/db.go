package database

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"teslaxy/models"
)

var DB *gorm.DB

func InitDB() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "."
	}

	dbPath := filepath.Join(configPath, "teslacam.db")
	var err error
	DB, err = gorm.Open("sqlite3", dbPath)
	if err != nil {
		fmt.Printf("Failed to connect to database at %s: %v\n", dbPath, err)
		panic("Failed to connect to database")
	}

	// ============================================================
	// AUTOMATIC DATABASE MIGRATIONS (Current Strategy)
	// ============================================================
	// This project uses GORM's AutoMigrate for automatic schema evolution.
	// This is the mechanism that makes migrations "automatic".
	//
	// How it works:
	// - On every startup, AutoMigrate is called with all model structs.
	// - GORM (jinzhu/gorm v1) will:
	//     * Create missing tables
	//     * Add missing columns (e.g. the new `source_dir` column on Clip)
	//     * Create indexes declared via `gorm:"index"` tags
	// - This is sufficient and automatic while the project is pre-v1.0
	//   and the data model is still evolving.
	//
	// IMPORTANT NOTES:
	// - New fields added to models (like `SourceDir string `gorm:"index"``)
	//   will be automatically created as columns + indexes on next startup.
	// - No manual SQL migrations are required for additive changes.
	// - This approach has known limitations (see docs/REVIEW.md).
	//   We intentionally keep it for developer velocity during heavy development.
	//
	// When a field is added in the future:
	//   1. Add the field + proper gorm tags to the model struct
	//   2. AutoMigrate will handle the rest on the next container/app restart.
	//   3. Update this comment if the migration strategy ever changes.
	//
	// Future improvement (post v1.0): Replace with golang-migrate or
	// gorm.io/gorm + separate migration files for full control.
	// ============================================================

	DB.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})
	fmt.Println("Database connection established and migrated (AutoMigrate complete)")
}

func CloseDB() {
	if DB != nil {
		DB.Close()
	}
}
