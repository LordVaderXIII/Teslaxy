package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var (
	secretKey     []byte
	adminUser     string
	adminPass     string
	loginAttempts = make(map[string]*loginAttempt)
	loginLock     sync.Mutex
)

type loginAttempt struct {
	count        int
	firstAttempt time.Time
}

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

	// Start rate limit cleanup
	go cleanupRateLimits()
}

func cleanupRateLimits() {
	for {
		time.Sleep(time.Minute)
		loginLock.Lock()
		for ip, attempt := range loginAttempts {
			if time.Since(attempt.firstAttempt) > time.Minute {
				delete(loginAttempts, ip)
			}
		}
		loginLock.Unlock()
	}
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

func checkRateLimit(ip string) bool {
	loginLock.Lock()
	defer loginLock.Unlock()

	attempt, exists := loginAttempts[ip]
	if !exists {
		loginAttempts[ip] = &loginAttempt{count: 1, firstAttempt: time.Now()}
		return true
	}

	// Reset if window passed (1 minute)
	if time.Since(attempt.firstAttempt) > time.Minute {
		attempt.count = 1
		attempt.firstAttempt = time.Now()
		return true
	}

	if attempt.count >= 5 {
		return false
	}

	attempt.count++
	return true
}

func Login(c *gin.Context) {
	// Rate Limiting
	if !checkRateLimit(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many login attempts. Please try again later."})
		return
	}

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
		log.Printf("AUTH: Successful login for user %q from IP %s", creds.Username, c.ClientIP())
		c.JSON(200, gin.H{"token": token})
	} else {
		log.Printf("AUTH: Failed login attempt for user %q from IP %s", creds.Username, c.ClientIP())
		c.JSON(401, gin.H{"error": "Invalid credentials"})
	}
}

// Claims represents the JWT claims used by Teslaxy.
// We use RegisteredClaims for proper exp, iat, nbf, iss, sub handling.
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// generateToken creates a signed JWT using the official golang-jwt/jwt/v5 library.
func generateToken(user string) (string, error) {
	claims := Claims{
		Username: user,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "teslaxy",
			Subject:   user,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secretKey)
}

// validateToken parses and validates a JWT using the official library.
// It performs algorithm verification, signature check, and expiration validation.
func validateToken(tokenString string) (bool, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Ensure the token method is HMAC (HS256) — prevents algorithm confusion attacks
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secretKey, nil
	})

	if err != nil {
		return false, err
	}

	if !token.Valid {
		return false, fmt.Errorf("invalid token")
	}

	return true, nil
}
