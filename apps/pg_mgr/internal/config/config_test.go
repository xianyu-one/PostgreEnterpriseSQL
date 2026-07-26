package config

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestConfigMigrationAndSerialization(t *testing.T) {
	// Create a temporary file for config
	tmpDir, err := ioutil.TempDir("", "pg_mgr_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	oldConfigContent := `
basedir: /app/custom_postgresql
instances:
  homedb:
    user: postgres
    datadir: /app/postgresql/home/data
    binpath: /app/postgresql/16/10/bin/postgres
    port: "51721"
`
	testConfPath := filepath.Join(tmpDir, "conf.yaml")
	err = ioutil.WriteFile(testConfPath, []byte(oldConfigContent), 0644)
	if err != nil {
		t.Fatalf("failed to write mock config: %v", err)
	}

	// Backup original config path and restore after test
	origPath := ConfigFilePath
	defer func() {
		ConfigFilePath = origPath
		viper.Reset()
	}()

	ConfigFilePath = testConfPath
	// Reset viper so it doesn't carry over states
	viper.Reset()

	// Initialize config
	InitConfig()

	// Check if migrated in memory
	if Global.BaseDir != "/app/custom_postgresql" {
		t.Errorf("expected BaseDir to be '/app/custom_postgresql', got '%s'", Global.BaseDir)
	}

	inst, ok := Global.Instances["homedb"]
	if !ok {
		t.Fatalf("expected instance 'homedb' in configuration")
	}

	if inst.DataDir != "/app/postgresql/home/data" {
		t.Errorf("expected DataDir to be '/app/postgresql/home/data', got '%s'", inst.DataDir)
	}
	if inst.BinPath != "/app/postgresql/16/10/bin/postgres" {
		t.Errorf("expected BinPath to be '/app/postgresql/16/10/bin/postgres', got '%s'", inst.BinPath)
	}

	// Verify that the config file has been migrated and old keys cleaned up / new keys written
	newContentBytes, err := ioutil.ReadFile(testConfPath)
	if err != nil {
		t.Fatalf("failed to read migrated config: %v", err)
	}
	newContent := string(newContentBytes)

	// Since it's saved with correct tags, it should contain 'base_dir', 'data_dir' and 'bin_path'
	if !strings.Contains(newContent, "base_dir:") {
		t.Errorf("expected migrated config file to contain 'base_dir:', file content:\n%s", newContent)
	}
	if !strings.Contains(newContent, "data_dir:") {
		t.Errorf("expected migrated config file to contain 'data_dir:', file content:\n%s", newContent)
	}
	if !strings.Contains(newContent, "bin_path:") {
		t.Errorf("expected migrated config file to contain 'bin_path:', file content:\n%s", newContent)
	}
	// And it should NOT contain the old fields 'basedir', 'datadir' and 'binpath'
	if strings.Contains(newContent, "basedir:") {
		t.Errorf("expected migrated config file NOT to contain 'basedir:', file content:\n%s", newContent)
	}
	if strings.Contains(newContent, "datadir:") {
		t.Errorf("expected migrated config file NOT to contain 'datadir:', file content:\n%s", newContent)
	}
	if strings.Contains(newContent, "binpath:") {
		t.Errorf("expected migrated config file NOT to contain 'binpath:', file content:\n%s", newContent)
	}
}

func TestNewConfigFieldsAndBackup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pg_mgr_new_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	origPath := ConfigFilePath
	defer func() {
		ConfigFilePath = origPath
		viper.Reset()
	}()

	ConfigFilePath = filepath.Join(tmpDir, "conf.yaml")
	viper.Reset()

	// 1. Test defaults
	InitConfig()
	if Global.LogDir != "/var/log/pg_mgr" {
		t.Errorf("expected default LogDir to be /var/log/pg_mgr, got '%s'", Global.LogDir)
	}
	if Global.LogLevel != "error" {
		t.Errorf("expected default LogLevel to be error, got '%s'", Global.LogLevel)
	}

	// 2. Test saving global config
	err = SaveGlobalConfig("/app/pg", "/var/custom/log", "debug")
	if err != nil {
		t.Fatalf("SaveGlobalConfig failed: %v", err)
	}

	// Re-init and check
	viper.Reset()
	InitConfig()
	if Global.BaseDir != "/app/pg" || Global.LogDir != "/var/custom/log" || Global.LogLevel != "debug" {
		t.Errorf("config values mismatch after save: %+v", Global)
	}

	// 3. Test saving instance pgrman config
	err = SaveInstanceToRegistryWithDatabaseConnection("testdb", "postgres", "/data", "/bin/postgres", "5432", "dbadmin", "appdb")
	if err != nil {
		t.Fatalf("SaveInstanceToRegistry failed: %v", err)
	}

	bk := &PgrmanConfig{
		Tool:           "pgrman",
		BackupDir:      "/backup",
		SrvLogPath:     "/data/log",
		ArcLogPath:     "/backup/archive",
		CompressData:   "YES",
		KeepArcLogDays: 5,
		KeepSrvLogDays: 10,
		KeepDataDays:   15,
		FullBackupCron: "0 2 * * 0",
		IncrBackupCron: "0 3 * * *",
		FullBackupDay:  1,
		FullBackupHour: 3,
		FullBackupMin:  30,
		IncrBackupHour: 4,
		IncrBackupMin:  0,
	}
	err = SaveInstancePgrmanConfig("testdb", bk)
	if err != nil {
		t.Fatalf("SaveInstancePgrmanConfig failed: %v", err)
	}

	// Reload config and verify
	viper.Reset()
	InitConfig()
	inst, ok := Global.Instances["testdb"]
	if !ok {
		t.Fatalf("testdb instance not found")
	}
	if inst.Pgrman == nil {
		t.Fatalf("pgrman config is nil")
	}
	if inst.Pgrman.Tool != "pgrman" || inst.Pgrman.BackupDir != "/backup" || inst.Pgrman.KeepArcLogDays != 5 || inst.Pgrman.FullBackupCron != "0 2 * * 0" {
		t.Errorf("pgrman config value mismatch: %+v", inst.Pgrman)
	}
	if inst.DatabaseUser != "dbadmin" {
		t.Errorf("database user mismatch: got %q", inst.DatabaseUser)
	}
	if inst.DatabaseName != "appdb" {
		t.Errorf("database name mismatch: got %q", inst.DatabaseName)
	}

	// Removing an instance must remove the whole entry, including nested backup
	// configuration, from both memory and the persisted registry.
	if err := RemoveInstanceFromRegistry("testdb"); err != nil {
		t.Fatalf("RemoveInstanceFromRegistry failed: %v", err)
	}
	if _, ok := Global.Instances["testdb"]; ok {
		t.Fatal("removed instance still exists in memory")
	}
	viper.Reset()
	InitConfig()
	if _, ok := Global.Instances["testdb"]; ok {
		t.Fatal("removed instance and its backup configuration still exist after reload")
	}
}
