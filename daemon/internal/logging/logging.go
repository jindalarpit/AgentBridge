// Package logging provides a configurable file logger with rotation for the daemon.
// It writes logs to a configurable file (default: ~/.agentbridge/daemon.log),
// rotates at 50 MB, and retains the 3 most recent rotated files.
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	// DefaultLogFileName is the default log file name.
	DefaultLogFileName = "daemon.log"

	// DefaultMaxSize is the default maximum log file size before rotation (50 MB).
	DefaultMaxSize = 50 * 1024 * 1024

	// DefaultMaxBackups is the default number of rotated files to retain.
	DefaultMaxBackups = 3
)

// Config holds the logging configuration.
type Config struct {
	// FilePath is the full path to the log file.
	// If empty, defaults to ~/.agentbridge/daemon.log.
	FilePath string

	// MaxSize is the maximum size in bytes before rotation.
	// Defaults to 50 MB.
	MaxSize int64

	// MaxBackups is the number of rotated files to retain.
	// Defaults to 3.
	MaxBackups int
}

// Logger wraps the standard log.Logger with file rotation support.
type Logger struct {
	mu         sync.Mutex
	file       *os.File
	filePath   string
	maxSize    int64
	maxBackups int
	size       int64
	logger     *log.Logger
}

// New creates a new Logger with the given configuration.
// It opens (or creates) the log file and sets up the standard log package
// to write to it.
func New(cfg Config) (*Logger, error) {
	filePath := cfg.FilePath
	if filePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to determine home directory: %w", err)
		}
		filePath = filepath.Join(home, ".agentbridge", DefaultLogFileName)
	}

	maxSize := cfg.MaxSize
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}

	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = DefaultMaxBackups
	}

	// Ensure the directory exists.
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory %s: %w", dir, err)
	}

	l := &Logger{
		filePath:   filePath,
		maxSize:    maxSize,
		maxBackups: maxBackups,
	}

	if err := l.openFile(); err != nil {
		return nil, err
	}

	l.logger = log.New(l, "", log.LstdFlags)

	return l, nil
}

// openFile opens the log file for appending, creating it if necessary.
// It also records the current file size for rotation tracking.
func (l *Logger) openFile() error {
	f, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", l.filePath, err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("failed to stat log file %s: %w", l.filePath, err)
	}

	l.file = f
	l.size = info.Size()
	return nil
}

// Write implements io.Writer. It writes data to the log file and rotates
// if the file exceeds the maximum size.
func (l *Logger) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if rotation is needed before writing.
	if l.size+int64(len(p)) > l.maxSize {
		if err := l.rotate(); err != nil {
			// If rotation fails, still try to write to the current file.
			fmt.Fprintf(os.Stderr, "log rotation failed: %v\n", err)
		}
	}

	n, err = l.file.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate performs log file rotation:
// 1. Close the current file
// 2. Shift existing rotated files (daemon.log.2 -> daemon.log.3, daemon.log.1 -> daemon.log.2)
// 3. Rename current file to daemon.log.1
// 4. Open a new log file
// 5. Remove any rotated files beyond maxBackups
func (l *Logger) rotate() error {
	// Close the current file.
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}

	// Shift existing backup files.
	// Remove the oldest if it would exceed maxBackups.
	oldestPath := fmt.Sprintf("%s.%d", l.filePath, l.maxBackups)
	_ = os.Remove(oldestPath)

	// Shift remaining files: .2 -> .3, .1 -> .2, etc.
	for i := l.maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", l.filePath, i)
		dst := fmt.Sprintf("%s.%d", l.filePath, i+1)
		// Ignore errors — file may not exist yet.
		_ = os.Rename(src, dst)
	}

	// Rename current log file to .1
	if err := os.Rename(l.filePath, fmt.Sprintf("%s.1", l.filePath)); err != nil {
		// If rename fails (e.g., file doesn't exist), just open a new file.
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to rename log file for rotation: %w", err)
		}
	}

	// Open a fresh log file.
	return l.openFile()
}

// Close closes the log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// StdLogger returns the underlying *log.Logger for use with standard log calls.
func (l *Logger) StdLogger() *log.Logger {
	return l.logger
}

// Writer returns the Logger as an io.Writer for use with other logging systems.
func (l *Logger) Writer() io.Writer {
	return l
}

// Printf logs a formatted message.
func (l *Logger) Printf(format string, v ...interface{}) {
	l.logger.Printf(format, v...)
}

// Println logs a message with a newline.
func (l *Logger) Println(v ...interface{}) {
	l.logger.Println(v...)
}

// Fatalf logs a formatted message and exits.
func (l *Logger) Fatalf(format string, v ...interface{}) {
	l.logger.Fatalf(format, v...)
}
