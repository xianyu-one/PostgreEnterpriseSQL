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
