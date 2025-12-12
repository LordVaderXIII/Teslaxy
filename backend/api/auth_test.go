package api

import (
	"os"
	"strings"
	"testing"
)

func TestTokenValidation(t *testing.T) {
	// Setup environment
	os.Setenv("JWT_SECRET", "test-secret")
	// Re-initialize secretKey because init() runs before main, but we want to be sure
	secretKey = []byte("test-secret")

	// 1. Test Valid Token
	token, err := generateToken("testuser")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	valid, err := validateToken(token)
	if !valid || err != nil {
		t.Errorf("Expected valid token, got invalid: %v", err)
	}

	// 2. Test Tampered Signature
	parts := strings.Split(token, ".")
	// Modify the signature (last part)
	// We'll just change the last character
	fakeSignature := parts[2]
	if len(fakeSignature) > 0 {
		// Change the last character to something else to invalidate the signature
		lastChar := fakeSignature[len(fakeSignature)-1]
		if lastChar == 'A' {
			fakeSignature = fakeSignature[:len(fakeSignature)-1] + "B"
		} else {
			fakeSignature = fakeSignature[:len(fakeSignature)-1] + "A"
		}
	}
	tamperedToken := parts[0] + "." + parts[1] + "." + fakeSignature

	valid, err = validateToken(tamperedToken)
	if valid {
		t.Error("Expected invalid token for tampered signature, got valid")
	}

	// 3. Test Invalid Base64 Signature (should fail gracefully)
	invalidBase64Token := parts[0] + "." + parts[1] + "." + "InvalidBase64!!!!"
	valid, err = validateToken(invalidBase64Token)
	if valid {
		t.Error("Expected invalid token for bad base64, got valid")
	}
}
