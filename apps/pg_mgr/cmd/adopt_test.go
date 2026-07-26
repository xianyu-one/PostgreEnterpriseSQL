package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"pg_mgr/internal/config"
	"pg_mgr/internal/utils"
)

func TestDetectUnstartedProperties(t *testing.T) {
	// Create a temporary data directory
	dataDir, err := os.MkdirTemp("", "pg_mgr_data_*")
	if err != nil {
		t.Fatalf("failed to create temp data dir: %v", err)
	}
	defer os.RemoveAll(dataDir)

	// Create a temporary base directory for postgres installations
	baseDir, err := os.MkdirTemp("", "pg_mgr_base_*")
	if err != nil {
		t.Fatalf("failed to create temp base dir: %v", err)
	}
	defer os.RemoveAll(baseDir)

	// Set global base dir to our temp base dir
	origBaseDir := config.Global.BaseDir
	config.Global.BaseDir = baseDir
	defer func() {
		config.Global.BaseDir = origBaseDir
	}()

	// 1. Create a mock installed postgres version
	mockBinDir := filepath.Join(baseDir, "16", "9", "bin")
	if err := os.MkdirAll(mockBinDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}
	mockBinPath := filepath.Join(mockBinDir, "postgres")
	if err := os.WriteFile(mockBinPath, []byte("mock binary"), 0755); err != nil {
		t.Fatalf("failed to write mock binary: %v", err)
	}
	incompatibleBinDir := filepath.Join(baseDir, "17", "10", "bin")
	if err := os.MkdirAll(incompatibleBinDir, 0755); err != nil {
		t.Fatalf("failed to create incompatible bin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(incompatibleBinDir, "postgres"), []byte("mock binary"), 0755); err != nil {
		t.Fatalf("failed to write incompatible mock binary: %v", err)
	}

	// 2. Create PG_VERSION in dataDir
	if err := os.WriteFile(filepath.Join(dataDir, "PG_VERSION"), []byte("16\n"), 0644); err != nil {
		t.Fatalf("failed to write PG_VERSION: %v", err)
	}

	// 3. Create postgresql.conf with port configuration in dataDir
	confContent := `
# Some comments
port = 5433
# listen_addresses = 'localhost'
`
	if err := os.WriteFile(filepath.Join(dataDir, "postgresql.conf"), []byte(confContent), 0644); err != nil {
		t.Fatalf("failed to write postgresql.conf: %v", err)
	}

	// Test compatible installed-version detection.
	compatible, err := compatibleInstalledVersions(dataDir, baseDir)
	if err != nil {
		t.Fatalf("compatibleInstalledVersions failed: %v", err)
	}
	if len(compatible) != 1 {
		t.Fatalf("expected one compatible installed version, got %d", len(compatible))
	}
	if detectedBinPath := postgresBinPath(baseDir, compatible[0]); detectedBinPath != mockBinPath {
		t.Errorf("expected detectedBinPath to be %s, got %s", mockBinPath, detectedBinPath)
	}

	// Test port detection logic
	detectedPort := ""
	confPath := filepath.Join(dataDir, "postgresql.conf")
	if _, err := os.Stat(confPath); err == nil {
		detectedPort = utils.ExtractRegexFromFile(confPath, `(?m)^#?port\s*=\s*(\d+)`)
	}
	if detectedPort != "5433" {
		t.Errorf("expected detectedPort to be 5433, got %s", detectedPort)
	}
}

func TestPrepareAdoptDataDirSetsPostgresPermissions(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.Chmod(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dataDir, "base")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatal(err)
	}

	if err := prepareAdoptDataDir(dataDir, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("prepareAdoptDataDir failed: %v", err)
	}
	info, err := os.Stat(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Fatalf("data directory permissions = %04o, want 0700", got)
	}
}
