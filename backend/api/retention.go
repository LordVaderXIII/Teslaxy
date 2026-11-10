package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	// Preserve the flat config contract; status is an additive bounded summary.
	c.JSON(http.StatusOK, struct {
		services.RetentionConfig
		Status services.RetentionStatus `json:"status"`
	}{svc.Config(), svc.Status()})
}

func getRetentionStorage(c *gin.Context) {
	svc := getRetentionService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retention service not initialized"})
		return
	}
	storage, err := svc.Storage()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, storage)
}

func putRetentionSettings(c *gin.Context) {
	svc := getRetentionService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retention service not initialized"})
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	var cfg services.RetentionConfig
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid retention settings"})
		return
	}
	// Mount relocation is an operator concern, never a settings API mutation.
	cfg.FootagePath = svc.Config().FootagePath
	if err := services.ValidateRetentionConfig(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := svc.SetConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, svc.Config())
}

func postRetentionRun(c *gin.Context) {
	svc := getRetentionService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retention service not initialized"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	cfg := svc.Config()
	if len(strings.TrimSpace(string(body))) > 0 {
		dryRun, err := parseStrictOptionalDryRun(body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if dryRun != nil {
			cfg.DryRun = *dryRun
			if *dryRun {
				cfg.Enabled = true
			}
		}
	} else if !cfg.Enabled && !cfg.DryRun {
		c.JSON(http.StatusBadRequest, gin.H{"error": "retention is disabled; enable it or pass dry_run=true"})
		return
	}

	if !cfg.Enabled && !cfg.DryRun {
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

// parseStrictOptionalDryRun accepts {"dry_run": true|false} only.
// Missing dry_run is OK (nil). Strings/numbers/null are errors.
func parseStrictOptionalDryRun(body []byte) (*bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	v, ok := raw["dry_run"]
	if !ok {
		return nil, nil
	}
	trimmed := strings.TrimSpace(string(v))
	switch trimmed {
	case "true":
		t := true
		return &t, nil
	case "false":
		f := false
		return &f, nil
	default:
		return nil, errInvalidDryRun
	}
}

var errInvalidDryRun = &dryRunError{msg: "dry_run must be a JSON boolean"}

type dryRunError struct{ msg string }

func (e *dryRunError) Error() string { return e.msg }

func setClipProtected(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid clip id"})
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	v, ok := raw["protected"]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "protected must be a JSON boolean"})
		return
	}
	trimmed := strings.TrimSpace(string(v))
	var protected bool
	switch trimmed {
	case "true":
		protected = true
	case "false":
		protected = false
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "protected must be a JSON boolean"})
		return
	}

	// The existing route remains valid for one clip. Optional ids adds merged
	// members to the same transaction; the route's anchor is always included.
	ids := []uint{uint(id)}
	if value, present := raw["ids"]; present {
		var members []uint
		if err := json.Unmarshal(value, &members); err != nil || len(members) == 0 || len(members) > 1000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ids must contain 1 to 1000 positive clip IDs"})
			return
		}
		seen := map[uint]bool{uint(id): true}
		for _, member := range members {
			if member == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid clip id"})
				return
			}
			if !seen[member] {
				ids = append(ids, member)
				seen[member] = true
			}
		}
	}

	svc := getRetentionService()
	apply := func() {
		tx := database.DB.Begin()
		if tx.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not begin protection update"})
			return
		}
		defer tx.Rollback()
		for _, member := range ids {
			var clip models.Clip
			if err := tx.First(&clip, member).Error; err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "clip not found"})
				return
			}
			if err := tx.Model(&clip).Update("protected", protected).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update protection"})
				return
			}
		}
		if err := tx.Commit().Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not commit protection update"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": id, "ids": ids, "protected": protected})
	}
	if svc != nil {
		svc.WithClipLock(apply)
		return
	}
	apply()
}
