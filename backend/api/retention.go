package api

import (
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"teslaxy/database"
	"teslaxy/models"
	"teslaxy/services"
)

var (
	retentionMu      sync.RWMutex
	retentionService *services.RetentionService
)

// SetRetentionService wires the process-wide retention service for HTTP handlers.
func SetRetentionService(svc *services.RetentionService) {
	retentionMu.Lock()
	retentionService = svc
	retentionMu.Unlock()
}

func getRetentionService() *services.RetentionService {
	retentionMu.RLock()
	defer retentionMu.RUnlock()
	return retentionService
}

func getRetentionSettings(c *gin.Context) {
	svc := getRetentionService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retention service not initialized"})
		return
	}
	if err := svc.ReloadConfig(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, svc.Config())
}

func putRetentionSettings(c *gin.Context) {
	svc := getRetentionService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retention service not initialized"})
		return
	}
	var cfg services.RetentionConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid retention settings"})
		return
	}
	if err := svc.SetConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, svc.Config())
}

type retentionRunRequest struct {
	DryRun *bool `json:"dry_run"`
}

func postRetentionRun(c *gin.Context) {
	svc := getRetentionService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retention service not initialized"})
		return
	}

	var req retentionRunRequest
	_ = c.ShouldBindJSON(&req)

	cfg := svc.Config()
	if req.DryRun != nil {
		cfg.DryRun = *req.DryRun
		if *req.DryRun {
			// Force evaluation without persisting; still never deletes.
			cfg.Enabled = true
		}
	} else if !cfg.Enabled && !cfg.DryRun {
		c.JSON(http.StatusBadRequest, gin.H{"error": "retention is disabled; enable it or pass dry_run=true"})
		return
	}

	report, err := svc.RunWithConfig(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "report": report})
		return
	}
	c.JSON(http.StatusOK, report)
}

// setClipProtected toggles the user protect/favourite flag on a clip.
func setClipProtected(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid clip id"})
		return
	}
	var body struct {
		Protected bool `json:"protected"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	var clip models.Clip
	if err := database.DB.First(&clip, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "clip not found"})
		return
	}
	if err := database.DB.Model(&clip).Update("protected", body.Protected).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": clip.ID, "protected": body.Protected})
}
