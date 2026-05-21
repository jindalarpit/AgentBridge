package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/user/agentbridge/daemon/internal/auth"
	"github.com/user/agentbridge/daemon/internal/config"
)

// CallbackServer manages the temporary HTTP server that receives the browser callback
// during the login flow. It listens on a random available port on localhost and waits
// for the browser to redirect back with the JWT token.
type CallbackServer struct {
	listener net.Listener
	state    string      // CSRF state parameter (32-char hex)
	tokenCh  chan string // receives JWT from callback
	errCh    chan error
	server   *http.Server
}

// NewCallbackServer creates a new CallbackServer bound to 127.0.0.1:0 (random available port).
// It generates a cryptographically random 32-character hex state string for CSRF protection.
func NewCallbackServer() (*CallbackServer, error) {
	// Bind to localhost on a random available port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start callback server: %w", err)
	}

	// Generate 16 random bytes → 32 hex characters for state parameter.
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to generate state parameter: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	cs := &CallbackServer{
		listener: listener,
		state:    state,
		tokenCh:  make(chan string, 1),
		errCh:    make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", cs.handleCallback)

	cs.server = &http.Server{
		Handler: mux,
	}

	return cs, nil
}

// Port returns the TCP port the callback server is listening on.
func (cs *CallbackServer) Port() int {
	return cs.listener.Addr().(*net.TCPAddr).Port
}

// State returns the CSRF state parameter (32-character hex string).
func (cs *CallbackServer) State() string {
	return cs.state
}

// Start begins serving HTTP requests on the callback server.
// It serves in a background goroutine and returns immediately.
func (cs *CallbackServer) Start() error {
	go func() {
		if err := cs.server.Serve(cs.listener); err != nil && err != http.ErrServerClosed {
			cs.errCh <- fmt.Errorf("callback server error: %w", err)
		}
	}()
	return nil
}

// handleCallback processes the browser redirect callback.
// It validates the state parameter matches and that a token is present,
// then sends the token on the channel and responds with a success HTML page.
func (cs *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state parameter matches.
	receivedState := r.URL.Query().Get("state")
	if receivedState != cs.state {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	// Validate token parameter is present.
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token parameter", http.StatusBadRequest)
		return
	}

	// Send token on channel (non-blocking since channel is buffered).
	select {
	case cs.tokenCh <- token:
	default:
		// Channel already has a token; ignore duplicate callbacks.
	}

	// Respond with success HTML page.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, successHTML)
}

// WaitForToken blocks until a token is received from the callback or the timeout expires.
// The default timeout for the login flow is 5 minutes.
func (cs *CallbackServer) WaitForToken(timeout time.Duration) (string, error) {
	select {
	case token := <-cs.tokenCh:
		return token, nil
	case err := <-cs.errCh:
		return "", err
	case <-time.After(timeout):
		return "", fmt.Errorf("login timed out after %v waiting for browser callback", timeout)
	}
}

// Close gracefully shuts down the HTTP server.
func (cs *CallbackServer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cs.server.Shutdown(ctx)
}

// successHTML is the HTML page displayed in the browser after a successful callback.
const successHTML = `<!DOCTYPE html>
<html>
<head>
    <title>Authentication Successful</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            margin: 0;
            background: #f8f9fa;
            color: #333;
        }
        .container {
            text-align: center;
            padding: 2rem;
        }
        h1 { color: #28a745; }
        p { color: #666; font-size: 1.1rem; }
    </style>
</head>
<body>
    <div class="container">
        <h1>&#10004; Authentication Successful</h1>
        <p>You can close this tab and return to your terminal.</p>
    </div>
</body>
</html>`

// runLogin orchestrates the browser-based login flow:
// 1. Start local callback server
// 2. Open browser to login page
// 3. Receive JWT via callback
// 4. Exchange JWT for daemon token
// 5. Persist token to config file
func runLogin(serverURL, appURL string) error {
	// Step 1: Create and start the callback server.
	cs, err := NewCallbackServer()
	if err != nil {
		return fmt.Errorf("failed to create callback server: %w", err)
	}
	defer cs.Close()

	if err := cs.Start(); err != nil {
		return fmt.Errorf("failed to start callback server: %w", err)
	}

	// Step 2: Construct the login URL.
	callbackURL := fmt.Sprintf("http://localhost:%d/callback", cs.Port())
	loginURL := fmt.Sprintf("%s?cli_callback=%s&cli_state=%s",
		appURL,
		url.QueryEscape(callbackURL),
		url.QueryEscape(cs.State()),
	)

	// Step 3: Try to open the browser.
	if err := openBrowser(loginURL); err != nil {
		// Fallback: print URL to stderr so user can open it manually.
		fmt.Fprintf(os.Stderr, "Could not open browser automatically. Please open this URL:\n%s\n", loginURL)
	}

	// Step 4: Wait for JWT token from callback (5-minute timeout).
	jwt, err := cs.WaitForToken(5 * time.Minute)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Step 5: Exchange JWT for daemon token.
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to determine hostname: %w", err)
	}
	tokenName := auth.FormatTokenName(hostname)

	client := auth.NewTokenClient(serverURL)
	ctx := context.Background()

	daemonToken, err := client.ExchangeJWT(ctx, jwt, tokenName, 90)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	// Step 6: Get user email (use the original JWT, not the daemon token).
	email, err := client.GetMe(ctx, jwt)
	if err != nil {
		return fmt.Errorf("failed to get user info: %w", err)
	}

	// Step 7: Save config.
	configPath, err := config.DefaultPath()
	if err != nil {
		return fmt.Errorf("failed to determine config path: %w", err)
	}

	// Load existing config to preserve extra fields.
	cfg, _ := config.Load(configPath)
	cfg.Token = daemonToken
	cfg.ServerURL = serverURL
	cfg.UserEmail = email

	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	// Step 8: Print confirmation.
	fmt.Printf("Authenticated as %s\n", email)
	return nil
}

// openBrowser attempts to open the given URL in the user's default browser.
// It uses platform-specific commands: "open" on macOS, "xdg-open" on Linux,
// and "cmd /c start" on Windows.
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
