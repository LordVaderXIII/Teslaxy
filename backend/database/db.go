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

	DB.AutoMigrate(&models.Clip{}, &models.VideoFile{}, &models.Telemetry{})
	fmt.Println("Database connection established and migrated")
}

func CloseDB() {
	if DB != nil {
		DB.Close()
	}
}
