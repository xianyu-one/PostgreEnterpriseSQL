package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"pg_mgr/internal/utils"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input   string
		wantStr string
		wantErr bool
	}{
		{"16.9", "16.9", false},
		{"17.0", "17.0", false},
		{"15.10", "15.10", false},
		{"16", "", true},
		{"16.a", "", true},
		{"a.9", "", true},
		{"16.9.1", "", true},
	}

	for _, tt := range tests {
		v, err := utils.ParseVersion(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if err == nil && v.Raw != tt.wantStr {
			t.Errorf("parseVersion(%q) got %q, want %q", tt.input, v.Raw, tt.wantStr)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int // -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
	}{
		{"16.9", "16.9", 0},
		{"16.9", "16.10", -1},
		{"16.10", "16.9", 1},
		{"16.9", "17.1", -1},
		{"17.1", "16.9", 1},
		{"15.8", "16.0", -1},
	}

	for _, tt := range tests {
		ver1, _ := utils.ParseVersion(tt.v1)
		ver2, _ := utils.ParseVersion(tt.v2)
		got := utils.CompareVersions(ver1, ver2)
		if tt.want == 0 && got != 0 {
			t.Errorf("compareVersions(%s, %s) = %d, want 0", tt.v1, tt.v2, got)
		} else if tt.want < 0 && got >= 0 {
			t.Errorf("compareVersions(%s, %s) = %d, want < 0", tt.v1, tt.v2, got)
		} else if tt.want > 0 && got <= 0 {
			t.Errorf("compareVersions(%s, %s) = %d, want > 0", tt.v1, tt.v2, got)
		}
	}
}

func TestGetInstalledVersions(t *testing.T) {
	// Create a temporary base directory
	tempDir, err := os.MkdirTemp("", "pg_mgr_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create some mock version structures
	// Valid version directories:
	// - tempDir/16/9/bin/postgres
	// - tempDir/16/10/bin/postgres
	// - tempDir/17/1/bin/postgres
	// Invalid directories/files:
	// - tempDir/15 (no minor dirs)
	// - tempDir/16/8/bin (no postgres file)
	// - tempDir/abc/9/bin/postgres (non-numeric major)
	// - tempDir/16/xyz/bin/postgres (non-numeric minor)

	dirsToCreate := []string{
		filepath.Join(tempDir, "16", "9", "bin"),
		filepath.Join(tempDir, "16", "10", "bin"),
		filepath.Join(tempDir, "17", "1", "bin"),
		filepath.Join(tempDir, "15"),
		filepath.Join(tempDir, "16", "8", "bin"),
		filepath.Join(tempDir, "abc", "9", "bin"),
		filepath.Join(tempDir, "16", "xyz", "bin"),
	}

	for _, d := range dirsToCreate {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create mock directory %s: %v", d, err)
		}
	}

	filesToCreate := []string{
		filepath.Join(tempDir, "16", "9", "bin", "postgres"),
		filepath.Join(tempDir, "16", "10", "bin", "postgres"),
		filepath.Join(tempDir, "17", "1", "bin", "postgres"),
		filepath.Join(tempDir, "abc", "9", "bin", "postgres"),
		filepath.Join(tempDir, "16", "xyz", "bin", "postgres"),
	}

	for _, f := range filesToCreate {
		if err := os.WriteFile(f, []byte("mock postgres binary"), 0755); err != nil {
			t.Fatalf("failed to create mock file %s: %v", f, err)
		}
	}

	versions, err := utils.GetInstalledVersions(tempDir)
	if err != nil {
		t.Fatalf("getInstalledVersions returned error: %v", err)
	}

	if len(versions) != 3 {
		t.Errorf("expected 3 installed versions, got %d", len(versions))
	}

	expectedRaw := []string{"16.9", "16.10", "17.1"}
	for i, v := range versions {
		if v.Raw != expectedRaw[i] {
			t.Errorf("at index %d, expected version %s, got %s", i, expectedRaw[i], v.Raw)
		}
	}
}

func TestGetVersionFromBinPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pg_mgr_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	binPath := filepath.Join(tempDir, "16", "9", "bin", "postgres")
	v, err := getVersionFromBinPath(tempDir, binPath, "mockUser")
	if err != nil {
		t.Fatalf("getVersionFromBinPath failed: %v", err)
	}

	if v.Raw != "16.9" || v.Major != 16 || v.Minor != 9 {
		t.Errorf("getVersionFromBinPath returned %v, expected 16.9", v)
	}
}

func TestParseConfValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"100MB", "100MB"},
		{"'hello'", "'hello'"},
		{"'hello''world'", "'hello''world'"},
		{"'hello' # comment", "'hello'"},
		{"100MB # comment", "100MB"},
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		got := parseConfValue(tt.input)
		if got != tt.want {
			t.Errorf("parseConfValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParsePostgresqlConf(t *testing.T) {
	content := `
# This is a comment
max_connections = 100
shared_buffers = 128MB # some buffer size
listen_addresses = '*'
# port = 5432
`
	params := parsePostgresqlConf(content)
	expected := map[string]string{
		"max_connections":  "100",
		"shared_buffers":   "128MB",
		"listen_addresses": "'*'",
	}

	if len(params) != len(expected) {
		t.Fatalf("expected %d params, got %d", len(expected), len(params))
	}

	for _, p := range params {
		val, ok := expected[p.Name]
		if !ok {
			t.Errorf("unexpected param name: %s", p.Name)
		} else if p.Value != val {
			t.Errorf("param %s: got %s, want %s", p.Name, p.Value, val)
		}
	}
}

func TestUpdatePostgresqlConfParam(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pg_mgr_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	confPath := filepath.Join(tempDir, "postgresql.conf")
	initialContent := `
max_connections = 100
# port = 5432
`
	err = os.WriteFile(confPath, []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	// Update existing uncommented param
	err = updatePostgresqlConfParam(confPath, "max_connections", "200")
	if err != nil {
		t.Fatalf("failed to update max_connections: %v", err)
	}

	// Add new param (or commented one)
	err = updatePostgresqlConfParam(confPath, "port", "5433")
	if err != nil {
		t.Fatalf("failed to update port: %v", err)
	}

	params, err := getConfParamsMap(confPath)
	if err != nil {
		t.Fatalf("getConfParamsMap failed: %v", err)
	}

	if params["max_connections"] != "200" {
		t.Errorf("expected max_connections=200, got %q", params["max_connections"])
	}
	if params["port"] != "5433" {
		t.Errorf("expected port=5433, got %q", params["port"])
	}
}

func TestParsePgHbaConf(t *testing.T) {
	content := `
# comment rule
local   all             all                                     trust
host    all             all             127.0.0.1/32            scram-sha-256
`
	rules := parsePgHbaConf(content)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0] != "local   all             all                                     trust" {
		t.Errorf("unexpected rule 0: %q", rules[0])
	}
}

func TestNormalizeSpaceAndIsRulePresent(t *testing.T) {
	newRules := []string{
		"local   all             all                                     trust",
		"host    all             all             127.0.0.1/32            scram-sha-256",
	}

	testRule1 := "local\tall\tall\ttrust"
	testRule2 := "host    all             all             127.0.0.1/32            md5"

	if !isRulePresent(newRules, testRule1) {
		t.Errorf("expected rule %q to be present", testRule1)
	}
	if isRulePresent(newRules, testRule2) {
		t.Errorf("expected rule %q to NOT be present", testRule2)
	}
}

func TestPgrmanUpgradeRename(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pg_mgr_upg_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldBackupDir := filepath.Join(tempDir, "backup/inst1")
	err = os.MkdirAll(oldBackupDir, 0755)
	if err != nil {
		t.Fatalf("failed to create old backup dir: %v", err)
	}
	dummyBackupFile := filepath.Join(oldBackupDir, "backup.tar")
	_ = os.WriteFile(dummyBackupFile, []byte("old backup data"), 0644)

	currentVer := "15.4"
	oldBackupDirArchived := oldBackupDir + "_old_" + currentVer

	if _, err := os.Stat(oldBackupDir); err == nil {
		_ = os.RemoveAll(oldBackupDirArchived)
		if err := os.Rename(oldBackupDir, oldBackupDirArchived); err != nil {
			t.Fatalf("failed to rename old backup dir: %v", err)
		}
	}

	if err := os.MkdirAll(oldBackupDir, 0755); err != nil {
		t.Fatalf("failed to create new backup dir: %v", err)
	}

	archivedFile := filepath.Join(oldBackupDirArchived, "backup.tar")
	if _, err := os.Stat(archivedFile); err != nil {
		t.Errorf("expected archived file to exist at %s", archivedFile)
	}

	entries, err := os.ReadDir(oldBackupDir)
	if err != nil || len(entries) != 0 {
		t.Errorf("expected new backup dir to be empty, got %d entries", len(entries))
	}
}
