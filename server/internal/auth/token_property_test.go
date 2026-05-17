package auth

import (
	"math"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"pgregory.net/rapid"
)

// **Validates: Requirements 3.2, 10.2**
// Property 20: Token Authentication Validity
// For any JWT token, the server SHALL accept it for WebSocket authentication
// if and only if it is well-formed, not expired (within 24 hours of issuance),
// and corresponds to an existing user. All other tokens SHALL be rejected with
// an authentication error.

const propertyTestSecret = "property-test-secret-key-2024"

// genUserID generates random non-empty user IDs.
func genUserID(t *rapid.T) string {
	return rapid.StringMatching(`[a-zA-Z0-9_-]{1,64}`).Draw(t, "userID")
}

// genEmail generates random email-like strings.
func genEmail(t *rapid.T) string {
	local := rapid.StringMatching(`[a-z0-9]{1,20}`).Draw(t, "local")
	domain := rapid.StringMatching(`[a-z0-9]{1,10}`).Draw(t, "domain")
	return local + "@" + domain + ".com"
}

// genSecret generates random non-empty secret strings.
func genSecret(t *rapid.T) string {
	return rapid.StringMatching(`[a-zA-Z0-9!@#$%^&*]{8,64}`).Draw(t, "secret")
}

// TestProperty_GenerateValidateRoundTrip verifies that for any valid userID and
// email, GenerateToken + ValidateToken round-trips correctly (claims match).
func TestProperty_GenerateValidateRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userID := genUserID(t)
		email := genEmail(t)
		secret := genSecret(t)

		tokenStr, err := GenerateToken(userID, email, secret)
		if err != nil {
			t.Fatalf("GenerateToken failed: %v", err)
		}

		claims, err := ValidateToken(tokenStr, secret)
		if err != nil {
			t.Fatalf("ValidateToken failed for valid token: %v", err)
		}

		if claims.UserID != userID {
			t.Fatalf("UserID mismatch: got %q, want %q", claims.UserID, userID)
		}
		if claims.Email != email {
			t.Fatalf("Email mismatch: got %q, want %q", claims.Email, email)
		}
	})
}

// TestProperty_WrongSecretAlwaysFails verifies that for any token generated with
// secret S, ValidateToken with a different secret S' always fails.
func TestProperty_WrongSecretAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userID := genUserID(t)
		email := genEmail(t)
		secret1 := genSecret(t)
		secret2 := genSecret(t)

		// Ensure secrets are different
		if secret1 == secret2 {
			secret2 = secret2 + "x"
		}

		tokenStr, err := GenerateToken(userID, email, secret1)
		if err != nil {
			t.Fatalf("GenerateToken failed: %v", err)
		}

		_, err = ValidateToken(tokenStr, secret2)
		if err == nil {
			t.Fatalf("ValidateToken should fail with wrong secret (secret1=%q, secret2=%q)", secret1, secret2)
		}
		if err != ErrInvalidToken {
			t.Fatalf("expected ErrInvalidToken, got: %v", err)
		}
	})
}

// TestProperty_ExpiredTokenAlwaysFails verifies that for any expired token
// (manually constructed with past expiry), ValidateToken always returns ErrTokenExpired.
func TestProperty_ExpiredTokenAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userID := genUserID(t)
		email := genEmail(t)
		secret := genSecret(t)

		// Generate a random past expiry between 1 second and 30 days ago
		expiredAgoSeconds := rapid.IntRange(1, 30*24*3600).Draw(t, "expiredAgoSeconds")
		now := time.Now()
		issuedAt := now.Add(-time.Duration(expiredAgoSeconds)*time.Second - TokenValidity)
		expiresAt := now.Add(-time.Duration(expiredAgoSeconds) * time.Second)

		claims := Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				IssuedAt:  jwt.NewNumericDate(issuedAt),
				ExpiresAt: jwt.NewNumericDate(expiresAt),
			},
			UserID: userID,
			Email:  email,
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, err := token.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("failed to sign token: %v", err)
		}

		_, err = ValidateToken(tokenStr, secret)
		if err == nil {
			t.Fatalf("ValidateToken should fail for expired token (expired %d seconds ago)", expiredAgoSeconds)
		}
		if err != ErrTokenExpired {
			t.Fatalf("expected ErrTokenExpired, got: %v", err)
		}
	})
}

// TestProperty_RandomStringAlwaysFails verifies that for any random string that
// is not a valid JWT, ValidateToken always returns ErrInvalidToken.
func TestProperty_RandomStringAlwaysFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random strings that are unlikely to be valid JWTs
		// Mix of printable ASCII characters, varying lengths
		randomStr := rapid.StringMatching(`[a-zA-Z0-9!@#$%^&*()_+={}\[\]:;"'<>,./?\-\\| ]{0,500}`).Draw(t, "randomStr")
		secret := genSecret(t)

		_, err := ValidateToken(randomStr, secret)
		if err == nil {
			t.Fatalf("ValidateToken should fail for random string: %q", randomStr)
		}
		if err != ErrInvalidToken {
			t.Fatalf("expected ErrInvalidToken for random string, got: %v", err)
		}
	})
}

// TestProperty_TokenExpiryWithin24Hours verifies that token expiry is always
// within 24 hours of issuance (±1 second tolerance).
func TestProperty_TokenExpiryWithin24Hours(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userID := genUserID(t)
		email := genEmail(t)
		secret := genSecret(t)

		before := time.Now()
		tokenStr, err := GenerateToken(userID, email, secret)
		if err != nil {
			t.Fatalf("GenerateToken failed: %v", err)
		}
		after := time.Now()

		claims, err := ValidateToken(tokenStr, secret)
		if err != nil {
			t.Fatalf("ValidateToken failed: %v", err)
		}

		// The expiry should be within 24 hours of issuance
		// IssuedAt should be between before and after
		issuedAt := claims.IssuedAt.Time
		expiresAt := claims.ExpiresAt.Time

		// Verify issuedAt is within the test execution window
		if issuedAt.Before(before.Add(-1*time.Second)) || issuedAt.After(after.Add(1*time.Second)) {
			t.Fatalf("IssuedAt %v not within expected window [%v, %v]", issuedAt, before, after)
		}

		// Verify expiry is exactly 24 hours from issuance (±1 second tolerance)
		expectedExpiry := issuedAt.Add(TokenValidity)
		diff := math.Abs(float64(expiresAt.Sub(expectedExpiry)))
		tolerance := float64(time.Second)

		if diff > tolerance {
			t.Fatalf("Token expiry %v differs from expected %v by %v (tolerance: 1s)",
				expiresAt, expectedExpiry, time.Duration(int64(diff)))
		}
	})
}
