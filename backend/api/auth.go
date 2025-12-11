package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var secretKey = []byte(os.Getenv("JWT_SECRET"))

func init() {
	if len(secretKey) == 0 {
		secretKey = []byte("default-secret-key-change-me")
	}
}

func AuthMiddleware() gin.HandlerFunc {
	enabled := os.Getenv("AUTH_ENABLED") == "true"
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}

		// Public Routes
		if c.Request.URL.Path == "/api/login" {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if valid, _ := validateToken(tokenString); !valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}

		c.Next()
	}
}

func Login(c *gin.Context) {
	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&creds); err != nil {
		c.JSON(400, gin.H{"error": "Bad request"})
		return
	}

	// Simple check (replace with DB check or env var)
    // Using simple defaults as requested for MVP
    adminUser := os.Getenv("ADMIN_USER")
    if adminUser == "" { adminUser = "admin" }
    adminPass := os.Getenv("ADMIN_PASS")
    if adminPass == "" { adminPass = "tesla" }

	if creds.Username == adminUser && creds.Password == adminPass {
		token, _ := generateToken(creds.Username)
		c.JSON(200, gin.H{"token": token})
	} else {
		c.JSON(401, gin.H{"error": "Invalid credentials"})
	}
}

// Simple JWT Implementation using stdlib
func generateToken(user string) (string, error) {
	header := `{"alg":"HS256","typ":"JWT"}`
	payload := fmt.Sprintf(`{"sub":"%s","exp":%d}`, user, time.Now().Add(24*time.Hour).Unix())

	encodedHeader := base64.RawURLEncoding.EncodeToString([]byte(header))
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))

	signatureInput := encodedHeader + "." + encodedPayload

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(signatureInput))
	signature := mac.Sum(nil)
	encodedSignature := base64.RawURLEncoding.EncodeToString(signature)

	return signatureInput + "." + encodedSignature, nil
}

func validateToken(token string) (bool, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false, fmt.Errorf("invalid token format")
	}

	signatureInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(signatureInput))
	expectedSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if expectedSignature != parts[2] {
		return false, nil
	}

	// Check exp
	payloadBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Exp int64 `json:"exp"`
	}
	json.Unmarshal(payloadBytes, &claims)
	if time.Now().Unix() > claims.Exp {
		return false, fmt.Errorf("token expired")
	}

	return true, nil
}
