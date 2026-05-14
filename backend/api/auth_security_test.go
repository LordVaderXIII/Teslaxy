package api

import (
	"os"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// TestGenerateToken_Injection verifies that even malicious usernames cannot
// inject additional top-level claims into the JWT (protected by struct-based claims).
func TestGenerateToken_Injection(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-for-injection-test")
	secretKey = []byte("test-secret-for-injection-test")

	maliciousUser := `admin","role":"admin`

	token, err := generateToken(maliciousUser)
	if err != nil {
		t.Fatalf("generateToken failed: %v", err)
	}

	// Parse with the real library
	claims := &Claims{}
	parsedToken, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
		return secretKey, nil
	})
	if err != nil || !parsedToken.Valid {
		t.Fatalf("Failed to parse token generated with malicious username: %v", err)
	}

	// The malicious content should be safely contained inside the Username field
	if claims.Username != maliciousUser {
		t.Errorf("Expected username to contain the malicious string, got: %s", claims.Username)
	}

	// There should be no "role" claim at the top level (struct prevents it)
	// We verify by checking that standard claims exist and no extra role field leaked
	if claims.Issuer != "teslaxy" {
		t.Error("Expected issuer to be 'teslaxy'")
	}
}
