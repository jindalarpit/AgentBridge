package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew_DefaultConfig(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	l, err := New(Config{FilePath: logPath})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer l.Close()

	if l.filePath != logPath {
		t.Errorf("filePath = %q, want %q", l.filePath, logPath)
	}
	if l.maxSize != DefaultMaxSize {
		t.Errorf("maxSize = %d, want %d", l.maxSize, DefaultMaxSize)
	}
	if l.maxBackups != DefaultMaxBackups {
		t.Errorf("maxBackups = %d, want %d", l.maxBackups, DefaultMaxBackups)
	}
}

func TestNew_CustomConfig(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "custom.log")

	l, err := New(Config{
		FilePath:   logPath,
		MaxSize:    1024,
		MaxBackups: 5,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer l.Close()

	if l.maxSize != 1024 {
		t.Errorf("maxSize = %d, want 1024", l.maxSize)
	}
	if l.maxBackups != 5 {
		t.Errorf("maxBackups = %d, want 5", l.maxBackups)
	}
}

func TestNew_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "subdir", "nested", "test.log")

	l, err := New(Config{FilePath: logPath})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer l.Close()

	// Verify directory was created.
	dir := filepath.Dir(logPath)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected directory at %s", dir)
	}
}

func TestLogger_Write(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	l, err := New(Config{FilePath: logPath})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer l.Close()

	msg := "hello world\n"
	n, err := l.Write([]byte(msg))
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if n != len(msg) {
		t.Errorf("Write() = %d, want %d", n, len(msg))
	}

	// Verify content was written.
	l.Close()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != msg {
		t.Errorf("file content = %q, want %q", string(data), msg)
	}
}

func TestLogger_Printf(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	l, err := New(Config{FilePath: logPath})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	l.Printf("test message %d", 42)
	l.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !strings.Contains(string(data), "test message 42") {
		t.Errorf("log output does not contain expected message, got: %q", string(data))
	}
}

func TestLogger_Rotation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Use a very small max size to trigger rotation quickly.
	l, err := New(Config{
		FilePath:   logPath,
		MaxSize:    100, // 100 bytes
		MaxBackups: 3,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer l.Close()

	// Write enough data to trigger rotation.
	// Each write is ~50 bytes, so after 3 writes we should have rotated.
	for i := 0; i < 5; i++ {
		msg := fmt.Sprintf("log message number %d with some padding data\n", i)
		_, err := l.Write([]byte(msg))
		if err != nil {
			t.Fatalf("Write() error on iteration %d: %v", i, err)
		}
	}

	l.Close()

	// Verify that rotated files exist.
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("current log file should exist: %v", err)
	}
	if _, err := os.Stat(logPath + ".1"); err != nil {
		t.Errorf("rotated file .1 should exist: %v", err)
	}
}

func TestLogger_RotationRetainsMaxBackups(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	maxBackups := 3
	l, err := New(Config{
		FilePath:   logPath,
		MaxSize:    50, // Very small to force many rotations.
		MaxBackups: maxBackups,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer l.Close()

	// Write enough data to trigger many rotations (more than maxBackups).
	for i := 0; i < 20; i++ {
		msg := fmt.Sprintf("log entry %d with padding to exceed limit\n", i)
		_, err := l.Write([]byte(msg))
		if err != nil {
			t.Fatalf("Write() error on iteration %d: %v", i, err)
		}
	}

	l.Close()

	// Verify that we have at most maxBackups rotated files.
	for i := 1; i <= maxBackups; i++ {
		rotatedPath := fmt.Sprintf("%s.%d", logPath, i)
		if _, err := os.Stat(rotatedPath); err != nil {
			t.Errorf("expected rotated file %s to exist: %v", rotatedPath, err)
		}
	}

	// Verify that file beyond maxBackups does NOT exist.
	beyondPath := fmt.Sprintf("%s.%d", logPath, maxBackups+1)
	if _, err := os.Stat(beyondPath); !os.IsNotExist(err) {
		t.Errorf("file %s should not exist (beyond maxBackups), err: %v", beyondPath, err)
	}
}

func TestLogger_Close(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	l, err := New(Config{FilePath: logPath})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Close should not error.
	if err := l.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}

	// Double close should not error.
	if err := l.Close(); err != nil {
		t.Errorf("second Close() error: %v", err)
	}
}

func TestLogger_StdLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	l, err := New(Config{FilePath: logPath})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer l.Close()

	stdLogger := l.StdLogger()
	if stdLogger == nil {
		t.Fatal("StdLogger() returned nil")
	}

	stdLogger.Println("test via std logger")
	l.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !strings.Contains(string(data), "test via std logger") {
		t.Errorf("log output does not contain expected message, got: %q", string(data))
	}
}

func TestLogger_AppendsToExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	// Write some initial content.
	if err := os.WriteFile(logPath, []byte("existing content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	l, err := New(Config{FilePath: logPath})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	l.Println("new content")
	l.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "existing content") {
		t.Error("existing content was lost")
	}
	if !strings.Contains(content, "new content") {
		t.Error("new content was not appended")
	}
}
