package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"teslaxy/database"
	"teslaxy/models"
	"teslaxy/services"
)

func SetupRoutes(r *gin.Engine) {
	api := r.Group("/api")

	// Login endpoint (public)
	api.POST("/login", Login)

	// Apply Auth Middleware
	api.Use(AuthMiddleware())

	{
		api.GET("/clips", getClips)
		api.GET("/clips/:id", getClipDetails)
		api.GET("/video/*path", serveVideo)

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
	if err := database.DB.Preload("VideoFiles").Order("timestamp desc").Limit(100).Find(&clips).Error; err != nil {
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

	// Security check: Prevent path traversal
	// Clean the path to resolve ".." and "."
	cleanPath := filepath.Clean(videoPath)

	// Get allowed footage path
	footagePath := os.Getenv("FOOTAGE_PATH")
	if footagePath == "" {
		footagePath = "/footage"
	}
	cleanFootagePath := filepath.Clean(footagePath)

	// Check if the request path is within the allowed footage directory
	// We use Rel to determine if the path is relative to footagePath
	rel, err := filepath.Rel(cleanFootagePath, cleanPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	c.File(cleanPath)
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
