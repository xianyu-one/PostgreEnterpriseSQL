package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pg_mgr/internal/config"
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

func TestRunPgUpgradeCommandReportsOutputAndDiagnosticDirectory(t *testing.T) {
	currentUser, err := utils.GetCurrentOSUser()
	if err != nil {
		t.Fatal(err)
	}
	diagnosticDir := filepath.Join(t.TempDir(), "pg_upgrade_output")
	err = runPgUpgradeCommand(currentUser, "printf 'incompatible extension'; exit 1", diagnosticDir)
	if err == nil {
		t.Fatal("expected pg_upgrade command to fail")
	}
	for _, want := range []string{"incompatible extension", diagnosticDir} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestBuildPgUpgradeCommandUsesConfiguredDatabaseRole(t *testing.T) {
	command := buildPgUpgradeCommand(
		"/tmp/diagnostics",
		"/new/lib:/old/lib",
		"/new/bin/pg_upgrade",
		"/data/old",
		"/data/new",
		"/old/bin",
		"/new/bin",
		"postgres",
	)
	if !strings.Contains(command, "-U 'postgres'") {
		t.Fatalf("command does not contain configured database role: %s", command)
	}
}

func TestBuildUpgradeInitDBCommandUsesSameConfiguredDatabaseRole(t *testing.T) {
	command := buildUpgradeInitDBCommand("/new/lib", "/new/bin/initdb", "/data/new", "postgres", "--no-data-checksums")
	for _, want := range []string{"-U 'postgres'", "--no-data-checksums"} {
		if !strings.Contains(command, want) {
			t.Fatalf("command does not contain %q: %s", want, command)
		}
	}
}

func TestArchivedBackupDirectoryIsSiblingWhenConfiguredPathHasTrailingSlash(t *testing.T) {
	got := archivedBackupDirectory("/srv/postgres/backup/", "17.11")
	want := "/srv/postgres/backup_old_17.11"
	if got != want {
		t.Fatalf("archivedBackupDirectory() = %q, want %q", got, want)
	}
}

func TestValidateMajorUpgradeWorkspaceRejectsExistingRecoveryDirectory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	recoveryDir := dataDir + "_old_17.11"
	if err := os.MkdirAll(recoveryDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := validateMajorUpgradeWorkspace(dataDir, recoveryDir, 17); err == nil {
		t.Fatal("expected existing recovery directory to block upgrade")
	}
}

func TestValidatePgRmanUpgradeWorkspaceRejectsExistingArchivedCatalog(t *testing.T) {
	backupDir := filepath.Join(t.TempDir(), "backup")
	archivedDir := archivedBackupDirectory(backupDir, "17.11")
	if err := os.MkdirAll(archivedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := validatePgRmanUpgradeWorkspace(backupDir, "17.11"); err == nil {
		t.Fatal("expected existing archived pg_rman catalog to block upgrade")
	}
}

func TestValidateMajorUpgradeWorkspaceRejectsMismatchedDataVersion(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "PG_VERSION"), []byte("18\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateMajorUpgradeWorkspace(dataDir, dataDir+"_old_17.11", 17); err == nil {
		t.Fatal("expected mismatched data major version to block upgrade")
	}
}

func TestRestoreMajorUpgradeDataDirectoryReplacesNewCluster(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	recoveryDir := filepath.Join(root, "data_old_17.11")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(recoveryDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "PG_VERSION"), []byte("18"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recoveryDir, "PG_VERSION"), []byte("17"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := restoreMajorUpgradeDataDirectory(dataDir, recoveryDir); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dataDir, "PG_VERSION"))
	if err != nil || string(content) != "17" {
		t.Fatalf("restored PG_VERSION = %q, error = %v", content, err)
	}
	if _, err := os.Stat(recoveryDir); !os.IsNotExist(err) {
		t.Fatalf("recovery directory still exists: %v", err)
	}
}

func TestRestorePgRmanBackupDirectoryReplacesPartialCatalog(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backup")
	archivedDir := filepath.Join(root, "backup_old_17.11")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(archivedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archivedDir, "backup.ini"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := restorePgRmanBackupDirectory(backupDir, archivedDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "backup.ini")); err != nil {
		t.Fatalf("original catalog was not restored: %v", err)
	}
}

func TestRestoreMajorUpgradeArtifactsRestoresDataAndBackupCatalog(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	oldDataDir := filepath.Join(root, "data_old_17.11")
	backupDir := filepath.Join(root, "backup")
	oldBackupDir := filepath.Join(root, "backup_old_17.11")
	for _, dir := range []string{dataDir, oldDataDir, backupDir, oldBackupDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldDataDir, "PG_VERSION"), []byte("17"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldBackupDir, "pg_rman.ini"), []byte("SRVLOG_PATH='/old/log'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := restoreMajorUpgradeArtifacts(dataDir, oldDataDir, backupDir, oldBackupDir, true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dataDir, "PG_VERSION"), filepath.Join(backupDir, "pg_rman.ini")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected restored artifact %s: %v", path, err)
		}
	}
	for _, path := range []string{oldDataDir, oldBackupDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("recovery directory still blocks retry: %s (%v)", path, err)
		}
	}
}

func TestFileSnapshotRestoresExistingFileAndRemovesCreatedFile(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	created := filepath.Join(root, "created")
	if err := os.WriteFile(existing, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	existingSnapshot, err := captureFileSnapshot(existing)
	if err != nil {
		t.Fatal(err)
	}
	createdSnapshot, err := captureFileSnapshot(created)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(existing, []byte("new"), 0644)
	_ = os.WriteFile(created, []byte("new"), 0644)
	if err := errors.Join(existingSnapshot.Restore(), createdSnapshot.Restore()); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(existing)
	if string(content) != "old" {
		t.Fatalf("restored content = %q", content)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created file still exists after rollback: %v", err)
	}
}

func TestUpgradeRequiresRootBeforePromptingOrChangingState(t *testing.T) {
	original := upgradeEnsureRoot
	t.Cleanup(func() { upgradeEnsureRoot = original })
	want := errors.New("root required")
	upgradeEnsureRoot = func() error { return want }
	if err := runUpgrade(); !errors.Is(err, want) {
		t.Fatalf("runUpgrade() error = %v, want %v", err, want)
	}
}

func TestRunPgRmanInitUsesUserAwareCommandRunner(t *testing.T) {
	original := runUpgradeCommandAsUser
	t.Cleanup(func() { runUpgradeCommandAsUser = original })

	called := false
	runUpgradeCommandAsUser = func(username, command string) (string, error) {
		called = true
		if username != "xianyu" {
			t.Fatalf("username = %q, want xianyu", username)
		}
		for _, want := range []string{"'/new/bin/pg_rman'", "-B '/backup/catalog'", "-D '/data/new'"} {
			if !strings.Contains(command, want) {
				t.Fatalf("command does not contain %q: %s", want, command)
			}
		}
		return "", nil
	}

	meta := config.InstanceMeta{DataDir: "/data/new", BinPath: "/new/bin/postgres"}
	if err := runPgRmanInit("xianyu", meta, "/new/bin/pg_rman", "/backup/catalog"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("user-aware command runner was not called")
	}
}

func TestParseDataChecksumState(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		enabled bool
	}{
		{name: "disabled", output: "Data page checksum version:           0\n", enabled: false},
		{name: "enabled", output: "Data page checksum version:           1\n", enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, err := parseDataChecksumState(tt.output)
			if err != nil {
				t.Fatal(err)
			}
			if enabled != tt.enabled {
				t.Fatalf("enabled = %v, want %v", enabled, tt.enabled)
			}
		})
	}
}

func TestParseInitDBChecksumCapabilities(t *testing.T) {
	capabilities := parseInitDBChecksumCapabilities("  -k, --data-checksums use checksums\n      --no-data-checksums disable checksums\n")
	if !capabilities.Enable || !capabilities.Disable {
		t.Fatalf("capabilities = %+v, want enable and disable", capabilities)
	}

	capabilities = parseInitDBChecksumCapabilities("  -k, --data-checksums use checksums\n")
	if !capabilities.Enable || capabilities.Disable {
		t.Fatalf("capabilities = %+v, want enable only", capabilities)
	}
}

func TestInitDBChecksumOptionMatchesOldClusterCapabilities(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		capabilities initDBChecksumCapabilities
		want         string
		wantErr      bool
	}{
		{name: "enabled low-version cluster remains enabled", enabled: true, capabilities: initDBChecksumCapabilities{Enable: true}, want: "--data-checksums"},
		{name: "disabled uses explicit target capability", enabled: false, capabilities: initDBChecksumCapabilities{Enable: true, Disable: true}, want: "--no-data-checksums"},
		{name: "disabled target without disable capability is verified after init", enabled: false, capabilities: initDBChecksumCapabilities{Enable: true}, want: ""},
		{name: "enabled target without enable capability fails", enabled: true, capabilities: initDBChecksumCapabilities{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := initDBChecksumOption(tt.enabled, tt.capabilities)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("option = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVerifyChecksumStateMatch(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		if err := verifyChecksumStateMatch(enabled, enabled); err != nil {
			t.Fatalf("matching state %v failed: %v", enabled, err)
		}
	}
	if err := verifyChecksumStateMatch(false, true); err == nil {
		t.Fatal("expected mismatched checksum state to fail")
	}
}

func TestHasManagedUpgradeBackup(t *testing.T) {
	tests := []struct {
		name string
		meta config.InstanceMeta
		want bool
	}{
		{name: "configured", meta: config.InstanceMeta{Pgrman: &config.PgrmanConfig{Tool: "pgrman", BackupDir: "/backup"}}, want: true},
		{name: "missing directory", meta: config.InstanceMeta{Pgrman: &config.PgrmanConfig{Tool: "pgrman"}}},
		{name: "different tool", meta: config.InstanceMeta{Pgrman: &config.PgrmanConfig{Tool: "other", BackupDir: "/backup"}}},
		{name: "not configured", meta: config.InstanceMeta{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasManagedUpgradeBackup(tt.meta); got != tt.want {
				t.Fatalf("hasManagedUpgradeBackup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNonInteractiveBackupBypassRequiresRiskAcknowledgement(t *testing.T) {
	previousUI := UI
	previousUpgrade := UpgConfig
	t.Cleanup(func() {
		UI = previousUI
		UpgConfig = previousUpgrade
	})

	UI.NonInteractive = true
	UpgConfig.InstanceName = "sales"
	meta := config.InstanceMeta{Pgrman: &config.PgrmanConfig{Tool: "pgrman", BackupDir: "/backup"}}
	if err := confirmUpgradeWithoutBackup(meta); err == nil || !strings.Contains(err.Error(), "--accept-no-backup-risk") {
		t.Fatalf("error = %v, want missing risk acknowledgement", err)
	}
	UpgConfig.AcceptNoBackupRisk = true
	if err := confirmUpgradeWithoutBackup(meta); err != nil {
		t.Fatalf("confirmed bypass failed: %v", err)
	}
}
