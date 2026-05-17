// Package daemonws manages daemon WebSocket connections (Daemon Hub).
//
// The Hub maintains a thread-safe map of active daemon connections keyed by
// daemon_id. Each connection runs separate read and write goroutines for
// concurrent message handling. Authentication is performed via Bearer token
// in the Authorization header before the WebSocket upgrade.
package daemonws
