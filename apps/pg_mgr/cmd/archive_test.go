package cmd

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"pg_mgr/internal/config"
	"pg_mgr/internal/utils"
)

func TestManagedArchiveCopyCommandIsIdempotentWhenWalAlreadyExists(t *testing.T) {
	got := managedArchiveCopyCommand("/archive/instance")
	want := "export PG_ARCHDIR=/archive/instance && (test -f $PG_ARCHDIR/%f || cp %p $PG_ARCHDIR/%f)"
	if got != want {
		t.Fatalf("managedArchiveCopyCommand() = %q, want %q", got, want)
	}
}

func TestArchiveManagement(t *testing.T) {
	oldCheck := archiveCheckRoot
	archiveCheckRoot = func() bool { return true }
	defer func() { archiveCheckRoot = oldCheck }()

	currUser, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "pg_mgr_archive_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	confPath := filepath.Join(dataDir, "postgresql.conf")
	initialConf := "archive_mode = off\narchive_command = 'test ! -f /user/custom/%f && cp %p /user/custom/%f'\n"
	if err := os.WriteFile(confPath, []byte(initialConf), 0644); err != nil {
		t.Fatalf("failed to write postgresql.conf: %v", err)
	}

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() {
		config.ConfigFilePath = "/etc/pg_mgr/conf.yaml"
	}()

	config.Global.Instances = make(map[string]config.InstanceMeta)
	config.Global.Instances["test-inst"] = config.InstanceMeta{
		User:    currUser.Username,
		DataDir: dataDir,
		BinPath: "/usr/bin/postgres",
		Port:    "5432",
	}

	archiveDir = filepath.Join(tempDir, "archive")
	archiveCommand = ""
	archiveSilent = true
	defer func() {
		archiveDir = ""
		archiveCommand = ""
		archiveSilent = false
	}()

	// 1. Enable archive
	runArchiveEnable("test-inst")

	arcMode, _ := utils.GetPostgresqlConfParam(confPath, "archive_mode")
	if arcMode != "on" {
		t.Errorf("expected archive_mode to be 'on', got '%s'", arcMode)
	}

	arcCmd, _ := utils.GetPostgresqlConfParam(confPath, "archive_command")
	userPart, pgMgrPart := utils.ParseArchiveCommand(arcCmd)

	if userPart != "test ! -f /user/custom/%f && cp %p /user/custom/%f" {
		t.Errorf("expected userPart to be preserved, got '%s'", userPart)
	}
	if !strings.Contains(pgMgrPart, filepath.Clean(archiveDir)) {
		t.Errorf("expected pgMgrPart to contain archive dir '%s', got '%s'", archiveDir, pgMgrPart)
	}

	// 2. Modify archive command via set
	archiveDir = ""
	archiveCommand = "cp %p /new/pgmgr/arch/%f"
	runArchiveEnable("test-inst")

	arcCmd2, _ := utils.GetPostgresqlConfParam(confPath, "archive_command")
	userPart2, pgMgrPart2 := utils.ParseArchiveCommand(arcCmd2)

	if userPart2 != "test ! -f /user/custom/%f && cp %p /user/custom/%f" {
		t.Errorf("expected userPart to remain unchanged, got '%s'", userPart2)
	}
	if pgMgrPart2 != "cp %p /new/pgmgr/arch/%f" {
		t.Errorf("expected pgMgrPart to be updated to 'cp %%p /new/pgmgr/arch/%%f', got '%s'", pgMgrPart2)
	}

	// 3. Disable archive
	archiveCommand = ""
	runArchiveDisable("test-inst")

	arcCmd3, _ := utils.GetPostgresqlConfParam(confPath, "archive_command")
	userPart3, pgMgrPart3 := utils.ParseArchiveCommand(arcCmd3)

	if userPart3 != "test ! -f /user/custom/%f && cp %p /user/custom/%f" {
		t.Errorf("expected userPart to remain after disable, got '%s'", userPart3)
	}
	if pgMgrPart3 != "" {
		t.Errorf("expected pgMgrPart to be empty after disable, got '%s'", pgMgrPart3)
	}
}

func TestArchiveMigration(t *testing.T) {
	oldCheck := archiveCheckRoot
	archiveCheckRoot = func() bool { return true }
	defer func() { archiveCheckRoot = oldCheck }()

	currUser, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "pg_mgr_archive_mig_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "data")
	oldArcDir := filepath.Join(tempDir, "old_arc")
	newArcDir := filepath.Join(tempDir, "new_arc")
	_ = os.MkdirAll(dataDir, 0755)
	_ = os.MkdirAll(oldArcDir, 0755)

	walFile := filepath.Join(oldArcDir, "000000010000000000000001")
	_ = os.WriteFile(walFile, []byte("wal content"), 0644)

	confPath := filepath.Join(dataDir, "postgresql.conf")
	initialConf := "archive_mode = on\narchive_command = 'true PG_MGR_ARCHIVE_START ; export PG_ARCHDIR=" + oldArcDir + " && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f && true PG_MGR_ARCHIVE_END'\n"
	_ = os.WriteFile(confPath, []byte(initialConf), 0644)

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() { config.ConfigFilePath = "/etc/pg_mgr/conf.yaml" }()

	config.Global.Instances = make(map[string]config.InstanceMeta)
	config.Global.Instances["arc-inst"] = config.InstanceMeta{
		User:    currUser.Username,
		DataDir: dataDir,
		BinPath: "/usr/bin/postgres",
		Port:    "5432",
	}

	archiveDir = newArcDir
	archiveCommand = ""
	archiveSilent = true
	archiveMigrate = true
	defer func() {
		archiveDir = ""
		archiveSilent = false
		archiveMigrate = false
	}()

	runArchiveEnable("arc-inst")

	if _, err := os.Stat(oldArcDir); !os.IsNotExist(err) {
		t.Errorf("expected old archive directory to be migrated/removed")
	}

	migratedWal := filepath.Join(newArcDir, "000000010000000000000001")
	if content, err := os.ReadFile(migratedWal); err != nil || string(content) != "wal content" {
		t.Errorf("expected WAL file to exist in new archive directory with correct content")
	}
}

func TestArchiveMigrationPgrmanSync(t *testing.T) {
	oldCheck := archiveCheckRoot
	archiveCheckRoot = func() bool { return true }
	defer func() { archiveCheckRoot = oldCheck }()

	currUser, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "pg_mgr_archive_pgrman_mig_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "data")
	backupDir := filepath.Join(tempDir, "backup")
	oldArcDir := filepath.Join(tempDir, "old_arc")
	newArcDir := filepath.Join(tempDir, "new_arc")

	_ = os.MkdirAll(dataDir, 0755)
	_ = os.MkdirAll(backupDir, 0755)
	_ = os.MkdirAll(oldArcDir, 0755)

	walFile := filepath.Join(oldArcDir, "000000010000000000000001")
	_ = os.WriteFile(walFile, []byte("wal content"), 0644)

	iniPath := filepath.Join(backupDir, "pg_rman.ini")
	initialIni := "SRVLOG_PATH='/tmp/srv'\nARCLOG_PATH='" + oldArcDir + "'\nCOMPRESS_DATA=YES\nKEEP_ARCLOG_DAYS=7\nKEEP_SRVLOG_DAYS=7\nKEEP_DATA_DAYS=30\n"
	_ = os.WriteFile(iniPath, []byte(initialIni), 0644)

	confPath := filepath.Join(dataDir, "postgresql.conf")
	initialConf := "archive_mode = on\narchive_command = 'true PG_MGR_ARCHIVE_START ; export PG_ARCHDIR=" + oldArcDir + " && test ! -f $PG_ARCHDIR/%f && cp %p $PG_ARCHDIR/%f && true PG_MGR_ARCHIVE_END'\n"
	_ = os.WriteFile(confPath, []byte(initialConf), 0644)

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() { config.ConfigFilePath = "/etc/pg_mgr/conf.yaml" }()

	config.Global.Instances = make(map[string]config.InstanceMeta)
	config.Global.Instances["arc-pgrman-inst"] = config.InstanceMeta{
		User:    currUser.Username,
		DataDir: dataDir,
		BinPath: "/usr/bin/postgres",
		Port:    "5432",
		Pgrman: &config.PgrmanConfig{
			Tool:           "pgrman",
			BackupDir:      backupDir,
			SrvLogPath:     "/tmp/srv",
			ArcLogPath:     oldArcDir,
			CompressData:   "YES",
			KeepArcLogDays: 7,
			KeepSrvLogDays: 7,
			KeepDataDays:   30,
		},
	}

	archiveDir = newArcDir
	archiveCommand = ""
	archiveSilent = true
	archiveMigrate = true
	defer func() {
		archiveDir = ""
		archiveSilent = false
		archiveMigrate = false
	}()

	runArchiveEnable("arc-pgrman-inst")

	// 1. Verify WAL migration
	if _, err := os.Stat(oldArcDir); !os.IsNotExist(err) {
		t.Errorf("expected old archive directory to be migrated/removed")
	}

	// 2. Verify config.Global Instance Pgrman ArcLogPath is updated
	instMeta := config.Global.Instances["arc-pgrman-inst"]
	if instMeta.Pgrman == nil || instMeta.Pgrman.ArcLogPath != newArcDir {
		t.Errorf("expected Pgrman.ArcLogPath to be updated to %s, got %v", newArcDir, instMeta.Pgrman)
	}

	// 3. Verify pg_rman.ini is updated with new ARCLOG_PATH
	iniContent, err := os.ReadFile(iniPath)
	if err != nil {
		t.Fatalf("failed to read pg_rman.ini: %v", err)
	}
	expectedLine := "ARCLOG_PATH='" + newArcDir + "'"
	if !strings.Contains(string(iniContent), expectedLine) {
		t.Errorf("expected pg_rman.ini to contain %s, got:\n%s", expectedLine, string(iniContent))
	}
}
