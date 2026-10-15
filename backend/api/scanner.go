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

// SetScannerService wires the process-wide scanner for HTTP handlers.
func SetScannerService(svc *services.ScannerService) {
	scannerMu.Lock()
	scannerService = svc
	scannerMu.Unlock()
}

func getScannerService() *services.ScannerService {
	scannerMu.RLock()
	defer scannerMu.RUnlock()
	return scannerService
}

// postPurgeMissing is the operator entry for ScannerService.PurgeMissing:
// soft-delete DB rows whose footage is gone under the configured root.
// Always requires auth + CSRF (MutationGuard), even when AUTH_ENABLED=false.
func postPurgeMissing(c *gin.Context) {
	svc := getScannerService()
	if svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scanner service not initialized"})
		return
	}
	res, err := svc.PurgeMissing()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "result": res})
		return
	}
	c.JSON(http.StatusOK, res)
}
