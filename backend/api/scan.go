package api

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"teslaxy/services"
)

var (
	scannerMu      sync.RWMutex
	scannerService *services.ScannerService
)

// SetScannerService wires the same scanner used by startup and file watching.
func SetScannerService(svc *services.ScannerService) {
	scannerMu.Lock()
	scannerService = svc
	scannerMu.Unlock()
}

func postScan(c *gin.Context) {
	scannerMu.RLock()
	svc := scannerService
	scannerMu.RUnlock()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scanner service not initialized"})
		return
	}
	if !svc.StartScan() {
		c.JSON(http.StatusConflict, gin.H{"error": "scan already running"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "scan started"})
}
