package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	secretKey []byte
	adminUser string
	adminPass string
)

func init() {
	// Initialize JWT Secret
	secretKey = []byte(os.Getenv("JWT_SECRET"))
	if len(secretKey) == 0 {
		// Generate random 32-byte key
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			panic(fmt.Sprintf("CRITICAL: Failed to generate random JWT secret: %v", err))
		}
		secretKey = key
		log.Println("SECURITY WARNING: JWT_SECRET not set. Using randomly generated secret key. All existing sessions will be invalidated on restart.")
	}

	// Initialize Admin Credentials
	loadAdminCreds()
}

func loadAdminCreds() {
	adminUser = os.Getenv("ADMIN_USER")
	if adminUser == "" {
		adminUser = "admin"
	}

	adminPass = os.Getenv("ADMIN_PASS")
	if adminPass == "" {
		// Generate secure random password
		bytes := make([]byte, 16)
		if _, err := rand.Read(bytes); err != nil {
			panic(fmt.Sprintf("CRITICAL: Failed to generate random admin password: %v", err))
		}
		adminPass = hex.EncodeToString(bytes)
		log.Printf("SECURITY NOTICE: ADMIN_PASS not set. Auto-generated admin password: %s", adminPass)
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

		tokenString := ""
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			// Fallback to query parameter for <img> and <video> tags
			tokenString = c.Query("token")
		}

		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

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

	// Verify using constant time compare to prevent timing attacks
	userMatch := subtle.ConstantTimeCompare([]byte(creds.Username), []byte(adminUser)) == 1
	passMatch := subtle.ConstantTimeCompare([]byte(creds.Password), []byte(adminPass)) == 1

	if userMatch && passMatch {
		token, _ := generateToken(creds.Username)
		c.JSON(200, gin.H{"token": token})
	} else {
		c.JSON(401, gin.H{"error": "Invalid credentials"})
	}
}

type jwtClaims struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
}

// Simple JWT Implementation using stdlib
func generateToken(user string) (string, error) {
	header := `{"alg":"HS256","typ":"JWT"}`

	claims := jwtClaims{
		Sub: user,
		Exp: time.Now().Add(24 * time.Hour).Unix(),
	}

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString([]byte(header))
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)

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
	expectedMAC := mac.Sum(nil)

	providedMAC, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false, nil
	}

	if !hmac.Equal(providedMAC, expectedMAC) {
		return false, nil
	}

	// Check exp
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false, fmt.Errorf("invalid token encoding")
	}

	var claims jwtClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return false, fmt.Errorf("invalid token payload")
	}

	if time.Now().Unix() > claims.Exp {
		return false, fmt.Errorf("token expired")
	}

	return true, nil
}
