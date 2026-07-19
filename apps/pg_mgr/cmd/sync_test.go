package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"pg_mgr/internal/config"
	"pg_mgr/internal/process"
)

func TestScanAndSyncInstancesPortMismatch(t *testing.T) {
	// Bypass root check
	oldCheck := checkRoot
	checkRoot = func() bool { return true }
	defer func() { checkRoot = oldCheck }()

	// Mock running processes
	tempDir, err := os.MkdirTemp("", "pg_mgr_sync_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	// Mock registry file path
	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() {
		config.ConfigFilePath = "/etc/pg_mgr/conf.yaml"
	}()

	// Register instance in global configuration
	config.Global.Instances = make(map[string]config.InstanceMeta)
	config.Global.Instances["sync-inst"] = config.InstanceMeta{
		User:    "postgres",
		DataDir: dataDir,
		BinPath: "/usr/bin/postgres",
		Port:    "5432",
	}

	// Set running process with mismatched port
	mockProcs := []process.PgProcess{
		{
			PID:     "12345",
			OSUser:  "postgres",
			Port:    "5433", // running port is 5433, but registered is 5432
			DataDir: dataDir,
			BinPath: "/usr/bin/postgres",
		},
	}

	oldFind := findPgProcesses
	findPgProcesses = func() []process.PgProcess {
		return mockProcs
	}
	defer func() { findPgProcesses = oldFind }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer r.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
	}()

	// Write "1\n" to select Option 1: Update existing registered instance
	go func() {
		defer w.Close()
		w.Write([]byte("1\n"))
	}()

	scanAndSyncInstances()

	// Verify that the port in YAML was updated to 5433
	meta, exists := config.Global.Instances["sync-inst"]
	if !exists {
		t.Fatalf("instance sync-inst not found in registry after sync")
	}
	if meta.Port != "5433" {
		t.Errorf("expected port in config to be 5433, got %s", meta.Port)
	}
}

func TestScanAndSyncInstancesDataDirMismatch(t *testing.T) {
	// Bypass root check
	oldCheck := checkRoot
	checkRoot = func() bool { return true }
	defer func() { checkRoot = oldCheck }()

	// Mock running processes
	tempDir, err := os.MkdirTemp("", "pg_mgr_sync_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir1 := filepath.Join(tempDir, "data1")
	dataDir2 := filepath.Join(tempDir, "data2")
	_ = os.MkdirAll(dataDir1, 0755)
	_ = os.MkdirAll(dataDir2, 0755)

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() {
		config.ConfigFilePath = "/etc/pg_mgr/conf.yaml"
	}()

	// Register instance with port 5432 and dataDir1
	config.Global.Instances = make(map[string]config.InstanceMeta)
	config.Global.Instances["sync-inst"] = config.InstanceMeta{
		User:    "postgres",
		DataDir: dataDir1,
		BinPath: "/usr/bin/postgres",
		Port:    "5432",
	}

	// Running process has port 5432 but dataDir2
	mockProcs := []process.PgProcess{
		{
			PID:     "12345",
			OSUser:  "postgres",
			Port:    "5432",
			DataDir: dataDir2,
			BinPath: "/usr/bin/postgres",
		},
	}

	oldFind := findPgProcesses
	findPgProcesses = func() []process.PgProcess {
		return mockProcs
	}
	defer func() { findPgProcesses = oldFind }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer r.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
	}()

	// Write "1\n" to select Option 1: Update existing registered instance
	go func() {
		defer w.Close()
		w.Write([]byte("1\n"))
	}()

	scanAndSyncInstances()

	// Verify that the data directory in YAML was updated to dataDir2
	meta, exists := config.Global.Instances["sync-inst"]
	if !exists {
		t.Fatalf("instance sync-inst not found in registry after sync")
	}
	if filepath.Clean(meta.DataDir) != filepath.Clean(dataDir2) {
		t.Errorf("expected DataDir in config to be %s, got %s", dataDir2, meta.DataDir)
	}
}
