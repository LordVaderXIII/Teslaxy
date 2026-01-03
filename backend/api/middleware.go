package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// Include ? or & before token to avoid matching part of a path or another param name suffix
var tokenRegex = regexp.MustCompile(`([?&]token=)[^&]*`)

// scrubLogPath removes sensitive query parameters from the log path
func scrubLogPath(path string) string {
	if !strings.Contains(path, "token=") {
		return path
	}
	// Use regex to replace value of token parameter until next & or end of string
	return tokenRegex.ReplaceAllString(path, "${1}***")
}

// SecureLogger returns a Gin Logger middleware that masks sensitive query params
func SecureLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("[GIN] %s | %3d | %13v | %15s | %-7s %s\n%s",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			scrubLogPath(param.Path),
			param.ErrorMessage,
		)
	})
}

// SecurityHeadersMiddleware adds common security headers to the response
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Prevent MIME sniffing
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		// Protect against clickjacking (allow same origin for potential iframe usage)
		c.Writer.Header().Set("X-Frame-Options", "SAMEORIGIN")
		// Enable XSS filtering in older browsers
		c.Writer.Header().Set("X-XSS-Protection", "1; mode=block")
		// Strict referrer policy to protect privacy
		c.Writer.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Content Security Policy (CSP)
		// - Allow 'self' by default
		// - Allow inline scripts/styles for React/Tailwind (unsafe-inline is a trade-off for SPA)
		// - Allow CartoDB basemaps for the map
		// - Allow blob: for 3D video textures
		c.Writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://*.basemaps.cartocdn.com; connect-src 'self'; media-src 'self' blob:; font-src 'self' data:; object-src 'none'; base-uri 'self';")
		c.Next()
	}
}

// CORSMiddleware enables CORS for specific endpoints (like video serving for 3D textures)
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// MaxBodySizeMiddleware limits the size of the request body to prevent DoS via memory exhaustion
func MaxBodySizeMiddleware(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}
