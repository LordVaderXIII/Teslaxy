package api

import "github.com/gin-gonic/gin"

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
