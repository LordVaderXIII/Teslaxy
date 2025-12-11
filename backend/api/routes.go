package api

import (
	"net/http"
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
	if err := database.DB.Order("timestamp desc").Limit(100).Find(&clips).Error; err != nil {
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

	// Security check
	if strings.Contains(videoPath, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid path"})
		return
	}

	c.File(videoPath)
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
