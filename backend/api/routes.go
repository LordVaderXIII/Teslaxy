package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"teslaxy/database"
	"teslaxy/models"
	"teslaxy/services"
)

func SetupRoutes(r *gin.Engine) {
	// Apply global security headers
	r.Use(SecurityHeadersMiddleware())

	// Limit request body size to 1MB to prevent DoS
	r.Use(MaxBodySizeMiddleware(1024 * 1024))

	api := r.Group("/api")

	// Login endpoint (public)
	api.POST("/login", Login)
	api.GET("/version", GetVersion)

	// Apply Auth Middleware
	api.Use(AuthMiddleware())

	{
		api.GET("/clips", getClips)
		api.GET("/clips/:id", getClipDetails)
		// Apply CORS only to video serving to support 3D textures (crossOrigin)
		api.GET("/video/*path", CORSMiddleware(), serveVideo)
		api.GET("/thumbnail/*path", getThumbnail)

		// Transcoding Status
		api.GET("/transcode/status", getTranscodeStatus)

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
	// Bolt: Optimize query by selecting only necessary fields for Clip, VideoFiles, and Telemetry.
	// This reduces payload size by excluding model timestamps (CreatedAt, UpdatedAt, DeletedAt)
	// and heavy fields (FullDataJson) from the list view.
	if err := database.DB.Select("id, timestamp, event_timestamp, event, city, telemetry_id").
		Preload("VideoFiles", func(db *gorm.DB) *gorm.DB {
			return db.Select("clip_id, camera, file_path, timestamp").Order("timestamp asc")
		}).
		Preload("Telemetry", func(db *gorm.DB) *gorm.DB {
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

func getTranscodeStatus(c *gin.Context) {
	status := services.GetTranscoderStatus()
	c.JSON(http.StatusOK, status)
}

func serveVideo(c *gin.Context) {
	videoPath := c.Param("path")
	quality := c.Query("quality")

	footagePath := os.Getenv("FOOTAGE_PATH")
	if footagePath == "" {
		footagePath = "/footage"
	}
	cleanFootagePath := filepath.Clean(footagePath)
	cleanRequestPath := filepath.Clean(videoPath)

	// Determine full path
	var fullPath string
	if strings.HasPrefix(cleanRequestPath, cleanFootagePath) {
		fullPath = cleanRequestPath
	} else {
		fullPath = filepath.Join(cleanFootagePath, cleanRequestPath)
	}

	// Security Check
	if fullPath != cleanFootagePath && !strings.HasPrefix(fullPath, cleanFootagePath+string(os.PathSeparator)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Verify file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		log.Printf("Error: Video file not found at %s", fullPath)
		c.JSON(http.StatusNotFound, gin.H{"error": "Video file not found"})
		return
	}

	// If quality is requested and not original, transcode
	if quality != "" && quality != "original" {
		cmd, stdout, release, err := services.GetTranscodeStream(c.Request.Context(), fullPath, quality)
		if err != nil {
			if err == services.ErrServerBusy {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Server busy, too many transcoding sessions"})
				return
			}
			log.Printf("Transcode error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Transcoding failed"})
			return
		}
		// Sentinel: Ensure semaphore slot is released when handler exits
		defer release()

		c.Header("Content-Type", "video/mp4")
		c.Status(http.StatusOK)

		if _, err := io.Copy(c.Writer, stdout); err != nil {
			log.Printf("Stream error: %v", err)
		}

		// Ensure process is cleaned up
		cmd.Wait()
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

	// Security: Validate input
	if err := req.Validate(); err != nil {
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
