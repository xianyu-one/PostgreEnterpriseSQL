package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

	// Test version detection logic
	detectedBinPath := ""
	pgVerBytes, err := os.ReadFile(filepath.Join(dataDir, "PG_VERSION"))
	if err != nil {
		t.Fatalf("failed to read PG_VERSION: %v", err)
	}
	majorStr := strings.TrimSpace(string(pgVerBytes))
	installed, err := utils.GetInstalledVersions(baseDir)
	if err != nil {
		t.Fatalf("failed to get installed versions: %v", err)
	}
	var matchingVersions []string
	for _, v := range installed {
		if strconv.Itoa(v.Major) == majorStr {
			matchingVersions = append(matchingVersions, filepath.Join(baseDir, strconv.Itoa(v.Major), strconv.Itoa(v.Minor), "bin", "postgres"))
		}
	}
	if len(matchingVersions) > 0 {
		detectedBinPath = matchingVersions[len(matchingVersions)-1]
	}

	if detectedBinPath != mockBinPath {
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
