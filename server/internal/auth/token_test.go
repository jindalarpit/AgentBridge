package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-key-for-unit-tests"

func TestGenerateToken(t *testing.T) {
	token, err := GenerateToken("user-123", "user@example.com", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken returned empty token")
	}
}

func TestValidateToken_Valid(t *testing.T) {
	token, err := GenerateToken("user-456", "test@example.com", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	claims, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}

	if claims.UserID != "user-456" {
		t.Errorf("expected UserID 'user-456', got '%s'", claims.UserID)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("expected Email 'test@example.com', got '%s'", claims.Email)
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	token, err := GenerateToken("user-789", "wrong@example.com", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	_, err = ValidateToken(token, "wrong-secret")
	if err == nil {
		t.Fatal("ValidateToken should have returned error for wrong secret")
	}
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got: %v", err)
	}
}

func TestValidateToken_Expired(t *testing.T) {
	// Create a token that's already expired by manually constructing claims
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now.Add(-48 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-24 * time.Hour)),
		},
		UserID: "user-expired",
		Email:  "expired@example.com",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	_, err = ValidateToken(tokenString, testSecret)
	if err == nil {
		t.Fatal("ValidateToken should have returned error for expired token")
	}
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got: %v", err)
	}
}

func TestValidateToken_Malformed(t *testing.T) {
	_, err := ValidateToken("not-a-valid-jwt", testSecret)
	if err == nil {
		t.Fatal("ValidateToken should have returned error for malformed token")
	}
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got: %v", err)
	}
}

func TestValidateToken_EmptyString(t *testing.T) {
	_, err := ValidateToken("", testSecret)
	if err == nil {
		t.Fatal("ValidateToken should have returned error for empty token")
	}
}

func TestRefreshToken_Valid(t *testing.T) {
	original, err := GenerateToken("user-refresh", "refresh@example.com", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	refreshed, err := RefreshToken(original, testSecret)
	if err != nil {
		t.Fatalf("RefreshToken returned error: %v", err)
	}

	if refreshed == "" {
		t.Fatal("RefreshToken returned empty token")
	}

	// Validate the refreshed token
	claims, err := ValidateToken(refreshed, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken on refreshed token returned error: %v", err)
	}

	if claims.UserID != "user-refresh" {
		t.Errorf("expected UserID 'user-refresh', got '%s'", claims.UserID)
	}
	if claims.Email != "refresh@example.com" {
		t.Errorf("expected Email 'refresh@example.com', got '%s'", claims.Email)
	}
}

func TestRefreshToken_InvalidToken(t *testing.T) {
	_, err := RefreshToken("invalid-token", testSecret)
	if err == nil {
		t.Fatal("RefreshToken should have returned error for invalid token")
	}
}

func TestRefreshToken_ExpiredToken(t *testing.T) {
	// Create an expired token
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now.Add(-48 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-24 * time.Hour)),
		},
		UserID: "user-expired",
		Email:  "expired@example.com",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	_, err = RefreshToken(tokenString, testSecret)
	if err == nil {
		t.Fatal("RefreshToken should have returned error for expired token")
	}
}

func TestGenerateToken_ClaimsExpiry(t *testing.T) {
	before := time.Now().Truncate(time.Second)
	token, err := GenerateToken("user-time", "time@example.com", testSecret)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	claims, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}

	// Verify expiry is approximately 24 hours from now.
	// JWT timestamps are second-precision, so we allow a 2-second window.
	expectedMin := before.Add(TokenValidity).Add(-1 * time.Second)
	expectedMax := before.Add(TokenValidity).Add(2 * time.Second)

	expiry := claims.ExpiresAt.Time
	if expiry.Before(expectedMin) || expiry.After(expectedMax) {
		t.Errorf("token expiry %v not within expected range [%v, %v]", expiry, expectedMin, expectedMax)
	}
}
