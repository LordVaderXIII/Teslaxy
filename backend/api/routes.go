package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"teslaxy/database"
	"teslaxy/models"
	"teslaxy/services"
)

func SetupRoutes(r *gin.Engine) {
	api := r.Group("/api")

	// Login endpoint (public)
	api.POST("/login", Login)
	api.GET("/version", GetVersion)

	// Apply Auth Middleware
	api.Use(AuthMiddleware())

	{
		api.GET("/clips", getClips)
		api.GET("/clips/:id", getClipDetails)
		api.GET("/video/*path", serveVideo)
		api.GET("/thumbnail/*path", getThumbnail)

		// Export Routes
		api.POST("/export", createExportJob)
		api.GET("/export/:jobID", getExportStatus)
		api.GET("/downloads/:filename", downloadExport)
	}
}

func getClips(c *gin.Context) {
	var clips []models.Clip
	// Pagination? For now, fetch latest 100
	// Optimized: Added index on Timestamp for faster sorting
	// Jules: Removed limit to show all historical clips as requested. Pagination can be added later if needed.
	// Bolt: Optimize query by selecting only necessary Telemetry fields (exclude heavy FullDataJson)
	if err := database.DB.Preload("VideoFiles").Preload("Telemetry", func(db *gorm.DB) *gorm.DB {
		return db.Select("id, clip_id, latitude, longitude")
	}).Order("timestamp desc").Find(&clips).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, clips)
}

func getClipDetails(c *gin.Context) {
	id := c.Param("id")
	var clip models.Clip
	if err := database.DB.Preload("VideoFiles").Preload("Telemetry").First(&clip, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Clip not found"})
		return
	}
	c.JSON(http.StatusOK, clip)
}

func serveVideo(c *gin.Context) {
	videoPath := c.Param("path")

	footagePath := os.Getenv("FOOTAGE_PATH")
	if footagePath == "" {
		footagePath = "/footage"
	}
	cleanFootagePath := filepath.Clean(footagePath)
	cleanRequestPath := filepath.Clean(videoPath)

	// Determine full path
	// If the request path already starts with the footage path, assume it's absolute
	// Otherwise, join it with the footage path
	var fullPath string
	if strings.HasPrefix(cleanRequestPath, cleanFootagePath) {
		fullPath = cleanRequestPath
	} else {
		fullPath = filepath.Join(cleanFootagePath, cleanRequestPath)
	}

	// Security Check
	// Ensure the resolved full path is strictly inside the footage path
	if fullPath != cleanFootagePath && !strings.HasPrefix(fullPath, cleanFootagePath+string(os.PathSeparator)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Verify file exists (add logging for debugging)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		// Log error for debugging
		log.Printf("Error: Video file not found at %s", fullPath)
		c.JSON(http.StatusNotFound, gin.H{"error": "Video file not found"})
		return
	}

	c.File(fullPath)
}

func createExportJob(c *gin.Context) {
	var req services.ExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jobID, err := services.QueueExport(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID, "status": "pending"})
}

func getExportStatus(c *gin.Context) {
	jobID := c.Param("jobID")
	status, exists := services.GetExportStatus(jobID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}
	c.JSON(http.StatusOK, status)
}

func downloadExport(c *gin.Context) {
	filename := c.Param("filename")

	// Security check
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
		return
	}

	filePath := filepath.Join("/config/exports", filename)
	c.File(filePath)
}
