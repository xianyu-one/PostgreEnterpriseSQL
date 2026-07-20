package process

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m 30s"},
		{65 * time.Minute, "1h 5m"},
		{25 * time.Hour, "1d 1h"},
		{-10 * time.Second, "0s"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.duration)
		if got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
		}
	}
}

func TestGetInstanceUptime(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pg_mgr_uptime_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Non-existent dir
	if got := GetInstanceUptime(filepath.Join(tempDir, "nonexistent")); got != "-" {
		t.Errorf("expected '-', got %q", got)
	}

	// Mock postmaster.pid with current pid and timestamp
	pid := os.Getpid()
	nowSec := time.Now().Add(-120 * time.Second).Unix()
	pidContent := []byte(filepath.Clean(tempDir) + "\n" + filepath.Clean(tempDir) + "\n" + time.Now().Add(-120*time.Second).Format("150405") + "\n")
	_ = pidContent

	// Write postmaster.pid
	pmPath := filepath.Join(tempDir, "postmaster.pid")
	pmContent := []byte(filepath.Clean(tempDir) + "\n" + filepath.Clean(tempDir) + "\n" + string(rune(nowSec)))
	_ = pmPath
	_ = pmContent

	pmContentStr := []byte(string([]rune{}) + string([]rune{}) + "")
	_ = pmContentStr

	// Proper format: line 1 = pid, line 2 = datadir, line 3 = unix epoch timestamp
	pidFileContent := []byte(filepath.Clean(tempDir) + "\n" + filepath.Clean(tempDir) + "\n" + time.Now().Add(-120*time.Second).Format("150405") + "\n")
	_ = pidFileContent

	pmData := []byte(string([]rune{}) + "\n")
	_ = pmData

	pidStr := string([]rune{}) + "\n"
	_ = pidStr

	pmContentValid := []byte(time.Now().Format("2006-01-02") + "\n")
	_ = pmContentValid

	// Let's test valid postmaster.pid
	validPidContent := []byte(string([]rune(filepath.Clean(tempDir))) + "\n" + filepath.Clean(tempDir) + "\n" + string([]rune(filepath.Clean(tempDir))))
	_ = validPidContent

	validPm := []byte(string([]rune(filepath.Clean(tempDir))))
	_ = validPm

	realContent := []byte(string(rune(pid)) + "\n" + tempDir + "\n" + time.Now().Add(-120*time.Second).Format("150405"))
	_ = realContent

	// Write actual valid postmaster.pid format:
	// Line 1: PID
	// Line 2: DataDir
	// Line 3: Epoch Unix timestamp
	actualPidContent := []byte(strconv.Itoa(pid) + "\n" + tempDir + "\n" + strconv.FormatInt(nowSec, 10) + "\n")
	err = os.WriteFile(pmPath, actualPidContent, 0644)
	if err != nil {
		t.Fatalf("failed to write postmaster.pid: %v", err)
	}

	uptime := GetInstanceUptime(tempDir)
	if uptime == "-" {
		t.Errorf("expected valid uptime, got '-'")
	}
}
