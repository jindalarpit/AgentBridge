package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/user/agentbridge/server/pkg/protocol"
)

// Domain errors for RuntimeService operations.
var (
	ErrDaemonNotFound  = errors.New("daemon not found")
	ErrRuntimeNotFound = errors.New("runtime not found")
	ErrRuntimeOffline  = errors.New("runtime is not available")
)

// Daemon represents a registered daemon instance.
type Daemon struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	DaemonID   string    `json:"daemon_id"`
	Status     string    `json:"status"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// Runtime represents a detected agent CLI on a daemon's machine.
type Runtime struct {
	ID         string    `json:"id"`
	DaemonID   string    `json:"daemon_id"`
	AgentType  string    `json:"agent_type"`
	BinaryPath string    `json:"binary_path"`
	Version    string    `json:"version"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// DaemonRegistration contains the data needed to register a daemon.
type DaemonRegistration struct {
	DaemonID string
	UserID   string
	Runtimes []protocol.RuntimeInfo
}

// RuntimeService manages daemon registrations and agent runtimes.
type RuntimeService interface {
	RegisterDaemon(ctx context.Context, reg DaemonRegistration) error
	DeregisterDaemon(ctx context.Context, daemonID string) error
	UpdateHeartbeat(ctx context.Context, daemonID string) error
	MarkOffline(ctx context.Context, daemonID string) error
	GetUserRuntimes(ctx context.Context, userID string) ([]Runtime, error)
	BindRuntime(ctx context.Context, sessionID, runtimeID string) error
}

// InMemoryRuntimeService implements RuntimeService with an in-memory store.
// It is safe for concurrent use.
type InMemoryRuntimeService struct {
	mu       sync.RWMutex
	daemons  map[string]*Daemon  // keyed by daemon_id
	runtimes map[string]*Runtime // keyed by runtime_id
	// bindings maps session_id -> runtime_id
	bindings map[string]string
	// runtimeCounter is used to generate unique runtime IDs
	runtimeCounter int
}

// NewInMemoryRuntimeService creates a new InMemoryRuntimeService.
func NewInMemoryRuntimeService() *InMemoryRuntimeService {
	return &InMemoryRuntimeService{
		daemons:  make(map[string]*Daemon),
		runtimes: make(map[string]*Runtime),
		bindings: make(map[string]string),
	}
}

// RegisterDaemon stores or updates a daemon record and replaces all runtimes for that daemon.
func (s *InMemoryRuntimeService) RegisterDaemon(_ context.Context, reg DaemonRegistration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Upsert daemon record.
	daemon, exists := s.daemons[reg.DaemonID]
	if exists {
		daemon.UserID = reg.UserID
		daemon.Status = "online"
		daemon.LastSeenAt = now
	} else {
		daemon = &Daemon{
			ID:         reg.DaemonID,
			UserID:     reg.UserID,
			DaemonID:   reg.DaemonID,
			Status:     "online",
			LastSeenAt: now,
			CreatedAt:  now,
		}
		s.daemons[reg.DaemonID] = daemon
	}

	// Remove all existing runtimes for this daemon.
	for id, rt := range s.runtimes {
		if rt.DaemonID == reg.DaemonID {
			delete(s.runtimes, id)
		}
	}

	// Add new runtimes from the registration.
	for _, ri := range reg.Runtimes {
		s.runtimeCounter++
		runtimeID := fmt.Sprintf("rt-%s-%d", reg.DaemonID, s.runtimeCounter)
		s.runtimes[runtimeID] = &Runtime{
			ID:         runtimeID,
			DaemonID:   reg.DaemonID,
			AgentType:  ri.AgentType,
			BinaryPath: ri.BinaryPath,
			Version:    ri.Version,
			Status:     ri.Status,
			CreatedAt:  now,
		}
	}

	return nil
}

// DeregisterDaemon marks a daemon and all its runtimes as offline.
func (s *InMemoryRuntimeService) DeregisterDaemon(_ context.Context, daemonID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	daemon, exists := s.daemons[daemonID]
	if !exists {
		return ErrDaemonNotFound
	}

	daemon.Status = "offline"

	for _, rt := range s.runtimes {
		if rt.DaemonID == daemonID {
			rt.Status = "offline"
		}
	}

	return nil
}

// UpdateHeartbeat updates the last_seen_at timestamp for a daemon.
func (s *InMemoryRuntimeService) UpdateHeartbeat(_ context.Context, daemonID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	daemon, exists := s.daemons[daemonID]
	if !exists {
		return ErrDaemonNotFound
	}

	daemon.LastSeenAt = time.Now()
	return nil
}

// MarkOffline marks a daemon and all its runtimes as offline.
// This is typically called by the heartbeat checker when a daemon misses heartbeats.
func (s *InMemoryRuntimeService) MarkOffline(_ context.Context, daemonID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	daemon, exists := s.daemons[daemonID]
	if !exists {
		return ErrDaemonNotFound
	}

	daemon.Status = "offline"

	for _, rt := range s.runtimes {
		if rt.DaemonID == daemonID {
			rt.Status = "offline"
		}
	}

	return nil
}

// GetUserRuntimes returns only runtimes with status "available" belonging to the user's online daemons.
func (s *InMemoryRuntimeService) GetUserRuntimes(_ context.Context, userID string) ([]Runtime, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect daemon IDs belonging to this user that are online.
	userDaemonIDs := make(map[string]bool)
	for _, d := range s.daemons {
		if d.UserID == userID && d.Status == "online" {
			userDaemonIDs[d.DaemonID] = true
		}
	}

	// Collect available runtimes for those daemons.
	var result []Runtime
	for _, rt := range s.runtimes {
		if userDaemonIDs[rt.DaemonID] && rt.Status == "available" {
			result = append(result, *rt)
		}
	}

	return result, nil
}

// GetDaemonByID returns the daemon record for a given daemon ID.
// Returns ErrDaemonNotFound if the daemon does not exist.
func (s *InMemoryRuntimeService) GetDaemonByID(_ context.Context, daemonID string) (*Daemon, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	daemon, exists := s.daemons[daemonID]
	if !exists {
		return nil, ErrDaemonNotFound
	}

	// Return a copy to prevent external mutation.
	result := *daemon
	return &result, nil
}

// GetRuntimeByID returns the runtime record for a given runtime ID.
// Returns ErrRuntimeNotFound if the runtime does not exist.
func (s *InMemoryRuntimeService) GetRuntimeByID(_ context.Context, runtimeID string) (*Runtime, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rt, exists := s.runtimes[runtimeID]
	if !exists {
		return nil, ErrRuntimeNotFound
	}

	// Return a copy to prevent external mutation.
	result := *rt
	return &result, nil
}

// BindRuntime validates that a runtime exists and is available, then stores the binding.
func (s *InMemoryRuntimeService) BindRuntime(_ context.Context, sessionID, runtimeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rt, exists := s.runtimes[runtimeID]
	if !exists {
		return ErrRuntimeNotFound
	}

	if rt.Status != "available" {
		return ErrRuntimeOffline
	}

	s.bindings[sessionID] = runtimeID
	return nil
}

// GetSessionBinding returns the runtime ID bound to a session.
// Returns ErrRuntimeNotFound if no binding exists.
func (s *InMemoryRuntimeService) GetSessionBinding(_ context.Context, sessionID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runtimeID, exists := s.bindings[sessionID]
	if !exists {
		return "", ErrRuntimeNotFound
	}
	return runtimeID, nil
}

// GetDaemonIDForUser returns the daemon IDs for a given user.
func (s *InMemoryRuntimeService) GetDaemonIDForUser(_ context.Context, userID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ids []string
	for _, d := range s.daemons {
		if d.UserID == userID {
			ids = append(ids, d.DaemonID)
		}
	}
	return ids
}
