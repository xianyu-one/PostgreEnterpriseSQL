package cmd

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"pg_mgr/internal/config"
)

func TestModifyInstancePort(t *testing.T) {
	// Bypass root check
	oldCheck := modifyCheckRoot
	modifyCheckRoot = func() bool { return true }
	defer func() { modifyCheckRoot = oldCheck }()

	// Get current user to avoid User lookup failures
	currUser, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}

	// Create temporary directories
	tempDir, err := os.MkdirTemp("", "pg_mgr_modify_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	// Write a mock postgresql.conf
	confPath := filepath.Join(dataDir, "postgresql.conf")
	initialConf := "# Some comment\nport = 5432\n"
	if err := os.WriteFile(confPath, []byte(initialConf), 0644); err != nil {
		t.Fatalf("failed to write postgresql.conf: %v", err)
	}

	// Mock registry file path
	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() {
		config.ConfigFilePath = "/etc/pg_mgr/conf.yaml"
	}()

	// Register instance in global configuration
	config.Global.Instances = make(map[string]config.InstanceMeta)
	config.Global.Instances["test-inst"] = config.InstanceMeta{
		User:    currUser.Username,
		DataDir: dataDir,
		BinPath: "/usr/bin/postgres",
		Port:    "5432",
	}

	// Set CLI variables
	modifyPort = "5433"
	modifyBinPath = ""
	modifyDataDir = ""
	modifyOSUser = ""
	defer func() {
		modifyPort = ""
	}()

	// Run modify logic
	runModify("test-inst")

	// Verify port is updated in config.Global
	meta, exists := config.Global.Instances["test-inst"]
	if !exists {
		t.Fatalf("instance test-inst not found in registry after modify")
	}
	if meta.Port != "5433" {
		t.Errorf("expected port in config to be 5433, got %s", meta.Port)
	}

	// Verify port is updated in postgresql.conf file
	confBytes, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("failed to read postgresql.conf: %v", err)
	}
	confStr := string(confBytes)

	// Our updatePostgresqlConfParam should have updated it
	params := parsePostgresqlConf(confStr)
	foundPort := false
	for _, p := range params {
		if p.Name == "port" {
			foundPort = true
			if p.Value != "5433" {
				t.Errorf("expected port to be 5433 in postgresql.conf, got %s", p.Value)
			}
		}
	}
	if !foundPort {
		t.Errorf("port param not found in postgresql.conf")
	}
}

func TestModifyInstanceOSUser(t *testing.T) {
	oldCheck := modifyCheckRoot
	modifyCheckRoot = func() bool { return true }
	defer func() { modifyCheckRoot = oldCheck }()
	oldWriteService := modifyWriteSystemdService
	modifyWriteSystemdService = func(string, string, string, string) error { return nil }
	defer func() { modifyWriteSystemdService = oldWriteService }()

	currUser, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "pg_mgr_modify_user_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "data")
	backupDir := filepath.Join(tempDir, "backup")
	_ = os.MkdirAll(dataDir, 0755)
	_ = os.MkdirAll(backupDir, 0755)

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() { config.ConfigFilePath = "/etc/pg_mgr/conf.yaml" }()

	config.Global.Instances = make(map[string]config.InstanceMeta)
	config.Global.Instances["test-user-inst"] = config.InstanceMeta{
		User:    currUser.Username,
		DataDir: dataDir,
		BinPath: "/usr/bin/postgres",
		Port:    "5432",
		Pgrman: &config.PgrmanConfig{
			BackupDir: backupDir,
		},
	}

	modifyPort = ""
	modifyBinPath = ""
	modifyDataDir = ""
	modifyOSUser = currUser.Username
	defer func() { modifyOSUser = "" }()

	runModify("test-user-inst")

	meta, exists := config.Global.Instances["test-user-inst"]
	if !exists {
		t.Fatalf("instance not found")
	}
	if meta.User != currUser.Username {
		t.Errorf("expected user %s, got %s", currUser.Username, meta.User)
	}
}

func TestModifyInstanceDataDirMigration(t *testing.T) {
	oldCheck := modifyCheckRoot
	modifyCheckRoot = func() bool { return true }
	defer func() { modifyCheckRoot = oldCheck }()
	oldWriteService := modifyWriteSystemdService
	oldStartService := modifyStartNewService
	var writtenDataDir string
	var registeredDataDirAtStart string
	started := false
	modifyWriteSystemdService = func(_, _, _, dataDir string) error {
		writtenDataDir = dataDir
		return nil
	}
	modifyStartNewService = func(_, _ string) error {
		started = true
		registeredDataDirAtStart = config.Global.Instances["mig-inst"].DataDir
		return nil
	}
	defer func() {
		modifyWriteSystemdService = oldWriteService
		modifyStartNewService = oldStartService
	}()

	currUser, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "pg_mgr_modify_mig_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldDataDir := filepath.Join(tempDir, "old_data")
	newDataDir := filepath.Join(tempDir, "new_data")
	_ = os.MkdirAll(oldDataDir, 0755)
	_ = os.WriteFile(filepath.Join(oldDataDir, "postgresql.conf"), []byte("port = 5432\n"), 0644)

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() { config.ConfigFilePath = "/etc/pg_mgr/conf.yaml" }()

	config.Global.Instances = make(map[string]config.InstanceMeta)
	config.Global.Instances["mig-inst"] = config.InstanceMeta{
		User:    currUser.Username,
		DataDir: oldDataDir,
		BinPath: "/usr/bin/postgres",
		Port:    "5432",
	}

	modifyPort = ""
	modifyBinPath = ""
	modifyDataDir = newDataDir
	modifyOSUser = ""
	modifyMigrate = true
	defer func() {
		modifyDataDir = ""
		modifyMigrate = false
	}()

	runModify("mig-inst")

	meta, exists := config.Global.Instances["mig-inst"]
	if !exists {
		t.Fatalf("instance not found in registry")
	}
	if meta.DataDir != newDataDir {
		t.Errorf("expected DataDir in registry to be %s, got %s", newDataDir, meta.DataDir)
	}
	if _, err := os.Stat(oldDataDir); !os.IsNotExist(err) {
		t.Errorf("expected oldDataDir to be migrated/removed")
	}
	if _, err := os.Stat(filepath.Join(newDataDir, "postgresql.conf")); err != nil {
		t.Errorf("expected postgresql.conf to exist in newDataDir")
	}
	if !started {
		t.Error("expected migrated instance to be startup-tested before saving")
	}
	if writtenDataDir != newDataDir {
		t.Errorf("startup test used data directory %q, want %q", writtenDataDir, newDataDir)
	}
	if registeredDataDirAtStart != oldDataDir {
		t.Errorf("configuration was committed before startup test: got %q, want old path %q", registeredDataDirAtStart, oldDataDir)
	}
}
