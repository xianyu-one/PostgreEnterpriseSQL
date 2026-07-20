package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pg_mgr/internal/config"
)

func TestGetCronExpr(t *testing.T) {
	bkNil := (*config.PgrmanConfig)(nil)
	if expr := getFullCronExpr(bkNil); expr != "0 2 * * 0" {
		t.Errorf("expected default full cron '0 2 * * 0', got '%s'", expr)
	}
	if expr := getIncrCronExpr(bkNil); expr != "0 3 * * *" {
		t.Errorf("expected default incr cron '0 3 * * *', got '%s'", expr)
	}

	bkCustom := &config.PgrmanConfig{
		FullBackupCron: "30 1 * * 6",
		IncrBackupCron: "15 4 * * *",
	}
	if expr := getFullCronExpr(bkCustom); expr != "30 1 * * 6" {
		t.Errorf("expected custom full cron '30 1 * * 6', got '%s'", expr)
	}
	if expr := getIncrCronExpr(bkCustom); expr != "15 4 * * *" {
		t.Errorf("expected custom incr cron '15 4 * * *', got '%s'", expr)
	}
}

func TestRunBackupList(t *testing.T) {
	oldRootCheck := ensureRootFunc
	ensureRootFunc = func() {}
	defer func() { ensureRootFunc = oldRootCheck }()

	// Mock config
	tempDir, err := os.MkdirTemp("", "pg_mgr_backup_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() {
		config.ConfigFilePath = "/etc/pg_mgr/conf.yaml"
	}()

	config.Global.Instances = map[string]config.InstanceMeta{
		"inst1": {
			User:    "postgres",
			DataDir: filepath.Join(tempDir, "inst1"),
			Port:    "5432",
			Pgrman: &config.PgrmanConfig{
				Tool:           "pgrman",
				BackupDir:      filepath.Join(tempDir, "backup/inst1"),
				FullBackupCron: "0 2 * * 0",
				IncrBackupCron: "0 3 * * *",
			},
		},
		"inst2": {
			User:    "postgres",
			DataDir: filepath.Join(tempDir, "inst2"),
			Port:    "5433",
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runBackupList()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "inst1") {
		t.Errorf("expected output to contain 'inst1', got:\n%s", output)
	}
	if !strings.Contains(output, "pg_rman") {
		t.Errorf("expected output to contain 'pg_rman', got:\n%s", output)
	}
	if !strings.Contains(output, "inst2") {
		t.Errorf("expected output to contain 'inst2', got:\n%s", output)
	}
}
