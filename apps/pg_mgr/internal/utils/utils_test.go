package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pg_mgr/internal/config"
)

func TestParseLogindRemoveIPC(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "systemd default", content: "[Login]\n#RemoveIPC=yes\n", want: "yes"},
		{name: "explicit no", content: "[Login]\nRemoveIPC=no\n", want: "no"},
		{name: "drop-in overrides main file", content: "[Login]\nRemoveIPC=yes\n# /etc/systemd/logind.conf.d/60-pg_mgr.conf\n[Login]\nRemoveIPC=no\n", want: "no"},
		{name: "ignore another section", content: "[Service]\nRemoveIPC=no\n[Login]\nRemoveIPC=yes\n", want: "yes"},
		{name: "boolean aliases", content: "[Login]\nRemoveIPC=off\n", want: "no"},
		{name: "invalid value", content: "[Login]\nRemoveIPC=maybe\n", want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseLogindRemoveIPC(tt.content); got != tt.want {
				t.Fatalf("ParseLogindRemoveIPC() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpdatePgrc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pgrc_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	pgrcPath := filepath.Join(tempDir, ".pgrc")

	// Case 1: File does not exist
	envs := map[string]string{
		"PG_VERSION_PATH": "'/app/postgresql/16/9'",
		"PGPORT":          "'5432'",
	}

	err = UpdatePgrc(pgrcPath, envs)
	if err != nil {
		t.Fatalf("UpdatePgrc failed for non-existent file: %v", err)
	}

	contentBytes, err := os.ReadFile(pgrcPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content := string(contentBytes)

	if !strings.Contains(content, "export PG_VERSION_PATH='/app/postgresql/16/9'") {
		t.Errorf("expected PG_VERSION_PATH, got: %s", content)
	}
	if !strings.Contains(content, "export PGPORT='5432'") {
		t.Errorf("expected PGPORT, got: %s", content)
	}

	// Case 2: File exists and contains custom content
	customContent := `# Custom comments here
alias pg_log='tail -f $PGDATA/log/postgresql.log'
export CUSTOM_VAR="custom_value"
export PGPORT='5433'
`
	err = os.WriteFile(pgrcPath, []byte(customContent), 0644)
	if err != nil {
		t.Fatalf("failed to write custom content: %v", err)
	}

	// Update PGPORT and add PG_VERSION_PATH
	envs = map[string]string{
		"PG_VERSION_PATH": "'/app/postgresql/17/1'",
		"PGPORT":          "'5434'",
	}

	err = UpdatePgrc(pgrcPath, envs)
	if err != nil {
		t.Fatalf("UpdatePgrc failed on update: %v", err)
	}

	contentBytes, err = os.ReadFile(pgrcPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content = string(contentBytes)

	// Check if custom comments and custom variables are preserved
	if !strings.Contains(content, "# Custom comments here") {
		t.Error("custom comments were not preserved")
	}
	if !strings.Contains(content, "alias pg_log='tail -f $PGDATA/log/postgresql.log'") {
		t.Error("custom alias was not preserved")
	}
	if !strings.Contains(content, `export CUSTOM_VAR="custom_value"`) {
		t.Error("custom environment variable was not preserved")
	}

	// Check if PGPORT was updated and PG_VERSION_PATH was appended
	if !strings.Contains(content, "export PGPORT='5434'") {
		t.Errorf("expected PGPORT to be updated to '5434', got: %s", content)
	}
	if strings.Contains(content, "export PGPORT='5433'") {
		t.Error("old PGPORT '5433' still exists")
	}
	if !strings.Contains(content, "export PG_VERSION_PATH='/app/postgresql/17/1'") {
		t.Errorf("expected PG_VERSION_PATH to be added, got: %s", content)
	}
}

func TestArchiveCommandParsingAndBuilding(t *testing.T) {
	// Case 1: No user command, only pg_mgr command
	cmd1 := BuildArchiveCommand("", "export PG_ARCHDIR=/arc && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f")
	if !strings.HasSuffix(cmd1, "&& true PG_MGR_ARCHIVE_END") {
		t.Errorf("expected tagBlock to end with '&& true PG_MGR_ARCHIVE_END', got: '%s'", cmd1)
	}
	user1, pgMgr1 := ParseArchiveCommand(cmd1)
	if user1 != "" {
		t.Errorf("expected empty userPart, got: '%s'", user1)
	}
	if pgMgr1 != "export PG_ARCHDIR=/arc && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f" {
		t.Errorf("expected pgMgrPart 'export PG_ARCHDIR=/arc && test ! -f $PG_ARCHDIR/%%f && cp %%p $PG_ARCHDIR/%%f', got: '%s'", pgMgr1)
	}

	// Case 2: Preserve user command when setting pg_mgr command
	userCmd := "test ! -f /user/arc/%f && cp %p /user/arc/%f"
	cmd2 := BuildArchiveCommand(userCmd, "export PG_ARCHDIR=/pgmgr/arc && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f")
	user2, pgMgr2 := ParseArchiveCommand(cmd2)
	if user2 != userCmd {
		t.Errorf("expected userPart '%s', got: '%s'", userCmd, user2)
	}
	if pgMgr2 != "export PG_ARCHDIR=/pgmgr/arc && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f" {
		t.Errorf("expected pgMgrPart 'export PG_ARCHDIR=/pgmgr/arc && test ! -f $PG_ARCHDIR/%%f && cp %%p $PG_ARCHDIR/%%f', got: '%s'", pgMgr2)
	}

	// Case 3: Update pg_mgr command preserving user command
	cmd3 := BuildArchiveCommand(user2, "export PG_ARCHDIR=/new/pgmgr/arc && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f")
	user3, pgMgr3 := ParseArchiveCommand(cmd3)
	if user3 != userCmd {
		t.Errorf("expected userPart '%s', got: '%s'", userCmd, user3)
	}
	if pgMgr3 != "export PG_ARCHDIR=/new/pgmgr/arc && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f" {
		t.Errorf("expected pgMgrPart 'export PG_ARCHDIR=/new/pgmgr/arc && test ! -f $PG_ARCHDIR/%%f && cp %%p $PG_ARCHDIR/%%f', got: '%s'", pgMgr3)
	}

	// Case 4: Disable/Remove pg_mgr command preserving user command
	cmd4 := BuildArchiveCommand(user3, "")
	user4, pgMgr4 := ParseArchiveCommand(cmd4)
	if user4 != userCmd {
		t.Errorf("expected userPart '%s', got: '%s'", userCmd, user4)
	}
	if pgMgr4 != "" {
		t.Errorf("expected empty pgMgrPart, got: '%s'", pgMgr4)
	}
}

func TestExtractArchiveDirFromCmd(t *testing.T) {
	cmdNew := "export PG_ARCHDIR=/app/postgresql/archive/homedb && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f"
	if dir := ExtractArchiveDirFromCmd(cmdNew); dir != "/app/postgresql/archive/homedb" {
		t.Errorf("expected '/app/postgresql/archive/homedb', got '%s'", dir)
	}

	cmdLegacy1 := "test ! -f /var/archive/inst1/%f && cp %p /var/archive/inst1/%f"
	if dir := ExtractArchiveDirFromCmd(cmdLegacy1); dir != "/var/archive/inst1" {
		t.Errorf("expected '/var/archive/inst1', got '%s'", dir)
	}

	cmdLegacy2 := "cp %p /custom/path/%f"
	if dir := ExtractArchiveDirFromCmd(cmdLegacy2); dir != "/custom/path" {
		t.Errorf("expected '/custom/path', got '%s'", dir)
	}
}

func TestGetPgMgrArchiveDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conf_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	confPath := filepath.Join(tempDir, "postgresql.conf")
	confContent := "archive_mode = on\narchive_command = 'true PG_MGR_ARCHIVE_START ; export PG_ARCHDIR=/app/postgresql/archive/homedb && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f && true PG_MGR_ARCHIVE_END'\n"
	if err := os.WriteFile(confPath, []byte(confContent), 0644); err != nil {
		t.Fatalf("failed to write conf: %v", err)
	}

	dir := GetPgMgrArchiveDir(confPath)
	if dir != "/app/postgresql/archive/homedb" {
		t.Errorf("expected '/app/postgresql/archive/homedb', got '%s'", dir)
	}
}

func TestGetInstanceBinDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bin_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	binDir := filepath.Join(tempDir, "bin")
	_ = os.MkdirAll(binDir, 0755)
	postgresFile := filepath.Join(binDir, "postgres")
	_ = os.WriteFile(postgresFile, []byte("fake binary"), 0755)

	meta1 := config.InstanceMeta{BinPath: postgresFile}
	if got := GetInstanceBinDir(meta1); got != binDir {
		t.Errorf("expected binDir '%s', got '%s'", binDir, got)
	}

	meta2 := config.InstanceMeta{BinPath: binDir}
	if got := GetInstanceBinDir(meta2); got != binDir {
		t.Errorf("expected binDir '%s', got '%s'", binDir, got)
	}
}

func TestGetPgrmanBin(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pgrman_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	binDir := filepath.Join(tempDir, "bin")
	_ = os.MkdirAll(binDir, 0755)
	postgresFile := filepath.Join(binDir, "postgres")
	pgrmanFile := filepath.Join(binDir, "pg_rman")
	_ = os.WriteFile(postgresFile, []byte("fake postgres"), 0755)
	_ = os.WriteFile(pgrmanFile, []byte("fake pg_rman"), 0755)

	meta := config.InstanceMeta{BinPath: postgresFile}
	if got := GetPgrmanBin(meta); got != pgrmanFile {
		t.Errorf("expected pgrman path '%s', got '%s'", pgrmanFile, got)
	}
}

func TestGetInstanceEnvPrefixAndBuildCmd(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cmd_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	binDir := filepath.Join(tempDir, "16/9/bin")
	libDir := filepath.Join(tempDir, "16/9/lib")
	_ = os.MkdirAll(binDir, 0755)
	_ = os.MkdirAll(libDir, 0755)
	postgresFile := filepath.Join(binDir, "postgres")
	_ = os.WriteFile(postgresFile, []byte("fake postgres"), 0755)

	meta := config.InstanceMeta{
		User:    "postgres",
		DataDir: filepath.Join(tempDir, "instances/inst1"),
		BinPath: postgresFile,
		Port:    "5432",
		Pgrman: &config.PgrmanConfig{
			BackupDir: filepath.Join(tempDir, "backup/inst1"),
		},
	}

	cmdStr := "pg_rman backup -B /backup"
	built := BuildInstanceCmd(meta, cmdStr)

	if !strings.Contains(built, "export PATH="+binDir+":$PATH") {
		t.Errorf("expected PATH export, got: %s", built)
	}
	if !strings.Contains(built, "export LD_LIBRARY_PATH="+libDir+":$LD_LIBRARY_PATH") {
		t.Errorf("expected LD_LIBRARY_PATH export, got: %s", built)
	}
	if !strings.Contains(built, "export PGDATA="+filepath.Join(tempDir, "instances/inst1")) {
		t.Errorf("expected PGDATA export, got: %s", built)
	}
	if !strings.Contains(built, "export PGPORT=5432") {
		t.Errorf("expected PGPORT export, got: %s", built)
	}
	if !strings.Contains(built, "export PG_RMAN_BACK_PATH="+filepath.Join(tempDir, "backup/inst1")) {
		t.Errorf("expected PG_RMAN_BACK_PATH export, got: %s", built)
	}
	if !strings.HasSuffix(built, " && "+cmdStr) {
		t.Errorf("expected command appended at end, got: %s", built)
	}
}

func TestMigrateDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "migrate_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldDir := filepath.Join(tempDir, "old_data")
	newDir := filepath.Join(tempDir, "new_data")

	_ = os.MkdirAll(filepath.Join(oldDir, "subdir"), 0755)
	file1 := filepath.Join(oldDir, "file1.txt")
	file2 := filepath.Join(oldDir, "subdir", "file2.txt")
	_ = os.WriteFile(file1, []byte("hello"), 0644)
	_ = os.WriteFile(file2, []byte("world"), 0644)

	err = MigrateDirectory(oldDir, newDir)
	if err != nil {
		t.Fatalf("MigrateDirectory failed: %v", err)
	}

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("expected oldDir to be removed, but it still exists")
	}

	newFile1 := filepath.Join(newDir, "file1.txt")
	newFile2 := filepath.Join(newDir, "subdir", "file2.txt")

	if content, err := os.ReadFile(newFile1); err != nil || string(content) != "hello" {
		t.Errorf("expected newFile1 content 'hello', got '%s', err: %v", string(content), err)
	}
	if content, err := os.ReadFile(newFile2); err != nil || string(content) != "world" {
		t.Errorf("expected newFile2 content 'world', got '%s', err: %v", string(content), err)
	}
}

func TestMigrateDirectoryToSubdirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "migrate_subdir_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldDir := filepath.Join(tempDir, "archive")
	newDir := filepath.Join(oldDir, "hkdb")

	_ = os.MkdirAll(filepath.Join(oldDir, "subdir"), 0755)
	file1 := filepath.Join(oldDir, "wal_001")
	file2 := filepath.Join(oldDir, "subdir", "wal_002")
	_ = os.WriteFile(file1, []byte("wal1"), 0644)
	_ = os.WriteFile(file2, []byte("wal2"), 0644)

	err = MigrateDirectory(oldDir, newDir)
	if err != nil {
		t.Fatalf("MigrateDirectory to subdirectory failed: %v", err)
	}

	newFile1 := filepath.Join(newDir, "wal_001")
	newFile2 := filepath.Join(newDir, "subdir", "wal_002")

	if content, err := os.ReadFile(newFile1); err != nil || string(content) != "wal1" {
		t.Errorf("expected newFile1 content 'wal1', got '%s', err: %v", string(content), err)
	}
	if content, err := os.ReadFile(newFile2); err != nil || string(content) != "wal2" {
		t.Errorf("expected newFile2 content 'wal2', got '%s', err: %v", string(content), err)
	}

	// Verify oldDir top-level files were cleaned up and only newDir exists inside oldDir
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		t.Fatalf("failed to read oldDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "hkdb" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only 'hkdb' in oldDir, got: %v", names)
	}
}

func TestCopyDirToSubdirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "copydir_subdir_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	srcDir := filepath.Join(tempDir, "src")
	subDir := filepath.Join(srcDir, "sub")

	_ = os.MkdirAll(srcDir, 0755)
	file1 := filepath.Join(srcDir, "file1.txt")
	_ = os.WriteFile(file1, []byte("data"), 0644)

	err = CopyDir(srcDir, subDir)
	if err != nil {
		t.Fatalf("CopyDir failed: %v", err)
	}

	copiedFile := filepath.Join(subDir, "file1.txt")
	if content, err := os.ReadFile(copiedFile); err != nil || string(content) != "data" {
		t.Errorf("expected copied content 'data', got '%s', err: %v", string(content), err)
	}

	// Ensure sub/sub does not exist (no infinite recursion)
	infiniteSub := filepath.Join(subDir, "sub")
	if _, err := os.Stat(infiniteSub); !os.IsNotExist(err) {
		t.Errorf("expected %s not to exist, but it exists", infiniteSub)
	}
}

func TestReadSelection(t *testing.T) {
	index, err := ReadSelection(strings.NewReader("2\n"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if index != 1 {
		t.Fatalf("index = %d, want 1", index)
	}
	if _, err := ReadSelection(strings.NewReader("4\n"), 3); err == nil {
		t.Fatal("expected an out-of-range selection error")
	}
}

func TestPathCompleterCompletesDirectoriesAndFiles(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tempDir, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "database.conf"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	input := []rune(filepath.Join(tempDir, "dat"))
	candidates, _ := (pathCompleter{}).Do(input, len(input))
	var values []string
	for _, candidate := range candidates {
		values = append(values, string(candidate))
	}
	joined := strings.Join(values, ",")
	if !strings.Contains(joined, "a"+string(os.PathSeparator)) || !strings.Contains(joined, "abase.conf") {
		t.Fatalf("unexpected path completion candidates: %q", values)
	}
}

func TestRunAsUserWithCombinedOutputDoesNotSuToCurrentUser(t *testing.T) {
	currentUser, err := GetCurrentOSUser()
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunAsUserWithCombinedOutput(currentUser, "printf direct-execution")
	if err != nil {
		t.Fatalf("command failed: %v, output: %s", err, out)
	}
	if out != "direct-execution" {
		t.Fatalf("output = %q, want direct-execution", out)
	}
}
