package api

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateToken_Injection(t *testing.T) {
	// Attempt to inject a claim or break JSON
	maliciousUser := `admin","role":"admin`

	token, err := generateToken(maliciousUser)
	if err != nil {
		t.Fatalf("generateToken failed: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("Invalid token format")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("Failed to decode payload: %v", err)
	}

	// Try to unmarshal into a map to see what happened
	var claims map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		// If unmarshal fails, it means we likely created invalid JSON (which is bad but maybe not exploitable as auth bypass, but causes DoS/Failure)
		// Or it means the fix isn't applied yet and we broke the JSON format.
		// If the fix IS applied, unmarshal should succeed and "sub" should contain the quotes.
		t.Logf("JSON Unmarshal failed (expected before fix for some inputs): %v", err)
	} else {
		// If unmarshal succeeds, check if "role" exists as a top-level key
		if _, ok := claims["role"]; ok {
			t.Fatalf("Security Vulnerability: Successfully injected 'role' claim into JWT")
		}

		// Check if 'sub' is exactly what we passed
		sub, ok := claims["sub"].(string)
		if !ok || sub != maliciousUser {
			t.Errorf("Sub claim mismatch. Expected '%s', got '%s'", maliciousUser, sub)
		}
	}
}
