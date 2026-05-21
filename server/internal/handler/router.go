package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/user/agentbridge/server/internal/auth"
	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/daemonws"
	mw "github.com/user/agentbridge/server/internal/middleware"
)

// RouterConfig holds configuration for the HTTP router.
type RouterConfig struct {
	// JWTSecret is the secret used for JWT token validation.
	JWTSecret string

	// CORSOrigins is a comma-separated list of allowed origins for CORS.
	// Use "*" to allow all origins.
	CORSOrigins string

	// RateLimitRPS is the maximum requests per second per IP for rate limiting.
	// A value of 0 disables rate limiting.
	RateLimitRPS int
}

// RouterDeps holds the dependencies needed to wire the HTTP router.
type RouterDeps struct {
	AuthHandler        *AuthHandler
	SessionHandler     *SessionHandler
	RuntimeHandler     *RuntimeHandler
	DaemonTokenHandler *DaemonTokenHandler
	ClientHub          *clientws.Hub
	DaemonHub          *daemonws.Hub
}

// NewRouter creates a Chi router with all middleware and routes mounted.
// It wires auth routes (public), session routes (authenticated), runtime routes
// (authenticated), and WebSocket upgrade endpoints for clients and daemons.
func NewRouter(cfg RouterConfig, deps RouterDeps) chi.Router {
	r := chi.NewRouter()

	// --- Global Middleware ---

	// Request ID for tracing.
	r.Use(chimiddleware.RequestID)

	// Real IP extraction (for rate limiting behind proxies).
	r.Use(chimiddleware.RealIP)

	// Structured logging.
	r.Use(loggingMiddleware)

	// Panic recovery — returns 500 instead of crashing the server.
	r.Use(chimiddleware.Recoverer)

	// CORS middleware.
	r.Use(corsMiddleware(cfg.CORSOrigins))

	// Rate limiting middleware.
	if cfg.RateLimitRPS > 0 {
		r.Use(rateLimitMiddleware(cfg.RateLimitRPS))
	}

	// --- Public Routes (no auth required) ---
	r.Route("/api/auth", func(r chi.Router) {
		r.Post("/register", deps.AuthHandler.Register)
		r.Post("/login", deps.AuthHandler.Login)
		r.Post("/refresh", deps.AuthHandler.Refresh)
		r.With(mw.AuthMiddleware(cfg.JWTSecret)).Get("/me", deps.AuthHandler.Me)
	})

	// --- Authenticated Routes ---
	r.Group(func(r chi.Router) {
		r.Use(mw.AuthMiddleware(cfg.JWTSecret))

		// Session routes.
		r.Route("/api/sessions", func(r chi.Router) {
			r.Post("/", deps.SessionHandler.CreateSession)
			r.Get("/", deps.SessionHandler.ListSessions)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", deps.SessionHandler.GetSession)
				r.Delete("/", deps.SessionHandler.DeleteSession)
				r.Patch("/", deps.SessionHandler.RenameSession)
				r.Get("/messages", deps.SessionHandler.GetMessages)
				r.Post("/messages", deps.SessionHandler.SendMessage)
				r.Post("/bind", deps.RuntimeHandler.BindRuntime)
			})
		})

		// Runtime routes.
		r.Get("/api/runtimes", deps.RuntimeHandler.ListRuntimes)

		// Daemon token routes.
		if deps.DaemonTokenHandler != nil {
			r.Post("/api/daemon-tokens", deps.DaemonTokenHandler.CreateToken)
		}
	})

	// --- Daemon Token Auth Routes (authenticated with ab_ token, not JWT) ---
	if deps.DaemonTokenHandler != nil {
		r.Delete("/api/daemon-tokens/current", deps.DaemonTokenHandler.RevokeCurrentToken)
	}

	// --- WebSocket Endpoints ---

	// Client WebSocket: /ws/client?token=<jwt>
	// Authentication is done via the token query parameter.
	r.Get("/ws/client", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, `{"error":"missing token query parameter"}`, http.StatusUnauthorized)
			return
		}

		claims, err := auth.ValidateToken(token, cfg.JWTSecret)
		if err != nil {
			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
			return
		}

		deps.ClientHub.HandleWebSocket(w, r, claims.UserID)
	})

	// Daemon WebSocket: /ws/daemon (Authorization: Bearer <token>)
	// Authentication is done via the Authorization header.
	// Supports both JWT tokens and ab_ daemon tokens.
	r.Get("/ws/daemon", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		token := daemonws.ExtractBearerToken(authHeader)
		if token == "" {
			http.Error(w, `{"error":"missing or invalid authorization header"}`, http.StatusUnauthorized)
			return
		}

		var userID string

		if strings.HasPrefix(token, "ab_") && deps.DaemonTokenHandler != nil && deps.DaemonTokenHandler.queries != nil {
			// Daemon token: hash-based lookup.
			tokenHash := sha256Hex(token)
			record, err := deps.DaemonTokenHandler.queries.GetDaemonTokenByHash(r.Context(), tokenHash)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}
			userID = record.UserID

			// Update last_used_at asynchronously.
			go deps.DaemonTokenHandler.queries.UpdateDaemonTokenLastUsed(context.Background(), record.ID)
		} else {
			// JWT token: validate signature.
			claims, err := auth.ValidateToken(token, cfg.JWTSecret)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}
			userID = claims.UserID
		}

		// The daemon identity uses the user ID from the token. The actual daemon_id
		// will be provided in the daemon:register message after the WebSocket is established.
		identity := daemonws.DaemonIdentity{
			DaemonID: "", // Will be set upon daemon:register message
			UserID:   userID,
		}

		deps.DaemonHub.HandleWebSocket(w, r, identity)
	})

	return r
}

// --- Middleware Implementations ---

// loggingMiddleware logs each HTTP request with method, path, status, and duration.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		log.Printf("http: %s %s %d %s",
			r.Method,
			r.URL.Path,
			ww.Status(),
			time.Since(start).Round(time.Millisecond),
		)
	})
}

// corsMiddleware returns a middleware that handles CORS preflight and response headers.
func corsMiddleware(allowedOrigins string) func(http.Handler) http.Handler {
	origins := parseOrigins(allowedOrigins)
	allowAll := len(origins) == 1 && origins[0] == "*"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" && (allowAll || isOriginAllowed(origin, origins)) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-Request-ID")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "3600")
			}

			// Handle preflight requests.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// parseOrigins splits a comma-separated origins string into a slice.
func parseOrigins(origins string) []string {
	if origins == "" {
		return []string{"*"}
	}
	parts := strings.Split(origins, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return []string{"*"}
	}
	return result
}

// isOriginAllowed checks if the given origin is in the allowed list.
func isOriginAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}

// rateLimitMiddleware returns a simple token-bucket rate limiter per IP address.
// It allows up to rps requests per second per IP, with a burst of 2*rps.
func rateLimitMiddleware(rps int) func(http.Handler) http.Handler {
	type visitor struct {
		tokens    float64
		lastSeen  time.Time
		maxTokens float64
		rate      float64
	}

	var (
		mu       sync.Mutex
		visitors = make(map[string]*visitor)
	)

	// Clean up stale entries every minute.
	go func() {
		for {
			time.Sleep(1 * time.Minute)
			mu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 3*time.Minute {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	burst := float64(rps * 2)
	rate := float64(rps)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			// Use X-Real-IP if available (set by RealIP middleware).
			if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
				ip = realIP
			}

			mu.Lock()
			v, exists := visitors[ip]
			now := time.Now()

			if !exists {
				v = &visitor{
					tokens:    burst,
					lastSeen:  now,
					maxTokens: burst,
					rate:      rate,
				}
				visitors[ip] = v
			}

			// Refill tokens based on elapsed time.
			elapsed := now.Sub(v.lastSeen).Seconds()
			v.tokens += elapsed * v.rate
			if v.tokens > v.maxTokens {
				v.tokens = v.maxTokens
			}
			v.lastSeen = now

			if v.tokens < 1 {
				mu.Unlock()
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			v.tokens--
			mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}

// sha256Hex computes the SHA-256 hash of a string and returns it as a lowercase hex string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
