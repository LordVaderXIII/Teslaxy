package api

import (
	"crypto/md5"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func getThumbnail(c *gin.Context) {
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

	// Final Security Check
	// Ensure the resolved full path is strictly inside the footage path
	if fullPath != cleanFootagePath && !strings.HasPrefix(fullPath, cleanFootagePath+string(os.PathSeparator)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Verify file exists at the full path
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Video file not found"})
		return
	}

	// 2. Cache Setup
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/config"
	}
	thumbDir := filepath.Join(configPath, "thumbnails")
	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		log.Printf("Failed to create thumbnail dir: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}

	// 3. Generate Cache Filename
	// Hash the full path to ensure uniqueness
	hash := md5.Sum([]byte(fullPath))
	hashStr := hex.EncodeToString(hash[:])
	thumbPath := filepath.Join(thumbDir, hashStr+".jpg")

	// 4. Check Cache
	if _, err := os.Stat(thumbPath); err == nil {
		c.File(thumbPath)
		return
	}

	// 5. Generate Thumbnail using FFmpeg
	// -ss 0.1: seek to 0.1s
	// -i input: input file
	// -vframes 1: output 1 frame
	// -vf scale=480:-1: resize to width 480 (keep aspect ratio)
	// -q:v 5: quality (1-31, lower is better)
	cmd := exec.Command("ffmpeg", "-y", "-ss", "0.1", "-i", fullPath, "-vframes", "1", "-vf", "scale=480:-1", "-q:v", "5", thumbPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("FFmpeg error: %v, Output: %s", err, string(out))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate thumbnail"})
		return
	}

	// 6. Serve
	c.File(thumbPath)
}
