package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/user/agentbridge/server/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// User represents a registered user stored in memory.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"display_name"`
	CreatedAt    time.Time `json:"created_at"`
}

// RegisterRequest is the JSON body for user registration.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginRequest is the JSON body for user login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AuthResponse is the JSON response returned on successful auth.
type AuthResponse struct {
	Token string `json:"token"`
	User  *User  `json:"user"`
}

// ErrorResponse is the JSON response for errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// AuthHandler handles authentication HTTP endpoints.
type AuthHandler struct {
	jwtSecret string
	mu        sync.RWMutex
	users     map[string]*User // keyed by email
	nextID    int
}

// NewAuthHandler creates a new AuthHandler with the given JWT secret.
func NewAuthHandler(jwtSecret string) *AuthHandler {
	return &AuthHandler{
		jwtSecret: jwtSecret,
		users:     make(map[string]*User),
		nextID:    1,
	}
}

// Register handles POST /api/auth/register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate email
	if !isValidEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "invalid email format")
		return
	}

	// Validate password
	if len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	// Check if user already exists
	h.mu.RLock()
	_, exists := h.users[req.Email]
	h.mu.RUnlock()

	if exists {
		writeError(w, http.StatusConflict, "user already exists")
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	// Create user
	h.mu.Lock()
	// Double-check after acquiring write lock
	if _, exists := h.users[req.Email]; exists {
		h.mu.Unlock()
		writeError(w, http.StatusConflict, "user already exists")
		return
	}

	userID := generateUserID(h.nextID)
	h.nextID++

	user := &User{
		ID:           userID,
		Email:        req.Email,
		PasswordHash: string(hash),
		DisplayName:  emailToDisplayName(req.Email),
		CreatedAt:    time.Now(),
	}
	h.users[req.Email] = user
	h.mu.Unlock()

	// Generate token
	token, err := auth.GenerateToken(user.ID, user.Email, h.jwtSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusCreated, AuthResponse{
		Token: token,
		User:  user,
	})
}

// Login handles POST /api/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Find user
	h.mu.RLock()
	user, exists := h.users[req.Email]
	h.mu.RUnlock()

	if !exists {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	// Compare password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	// Generate token
	token, err := auth.GenerateToken(user.ID, user.Email, h.jwtSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{
		Token: token,
		User:  user,
	})
}

// Refresh handles POST /api/auth/refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tokenString := extractBearerToken(r)
	if tokenString == "" {
		writeError(w, http.StatusUnauthorized, "missing authorization token")
		return
	}

	newToken, err := auth.RefreshToken(tokenString, h.jwtSecret)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"token": newToken,
	})
}

// Me handles GET /api/auth/me.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract user ID from context (set by auth middleware)
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Find user by ID
	h.mu.RLock()
	var foundUser *User
	for _, u := range h.users {
		if u.ID == userID {
			foundUser = u
			break
		}
	}
	h.mu.RUnlock()

	if foundUser == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, foundUser)
}

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

// UserIDKey is the context key for the authenticated user's ID.
const UserIDKey contextKey = "userID"

// isValidEmail performs basic email validation (contains @ and .).
func isValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}
	atIdx := strings.Index(email, "@")
	if atIdx < 1 {
		return false
	}
	domain := email[atIdx+1:]
	if len(domain) < 3 {
		return false
	}
	dotIdx := strings.Index(domain, ".")
	if dotIdx < 1 || dotIdx >= len(domain)-1 {
		return false
	}
	return true
}

// extractBearerToken extracts the token from the Authorization header.
func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// generateUserID creates a simple user ID from a counter.
func generateUserID(n int) string {
	return "user-" + strings.Replace(time.Now().Format("20060102150405"), " ", "", -1) + "-" + itoa(n)
}

// itoa converts an int to a string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}

// emailToDisplayName extracts a display name from an email address.
func emailToDisplayName(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) > 0 {
		return parts[0]
	}
	return email
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}
