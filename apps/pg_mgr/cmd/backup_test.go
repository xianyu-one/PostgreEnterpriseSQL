package cmd

import (
	"bytes"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"pg_mgr/internal/config"
)

func TestGetCronExpr(t *testing.T) {
	bkNil := (*config.PgrmanConfig)(nil)
	if expr := getFullCronExpr(bkNil); expr != "0 2 * * 0" {
		t.Errorf("expected default full cron '0 2 * * 0', got '%s'", expr)
	}
	if expr := getIncrCronExpr(bkNil); expr != "0 3 * * *" {
		t.Errorf("expected default incr cron '0 3 * * *', got '%s'", expr)
	}

	bkCustom := &config.PgrmanConfig{
		FullBackupCron: "30 1 * * 6",
		IncrBackupCron: "15 4 * * *",
	}
	if expr := getFullCronExpr(bkCustom); expr != "30 1 * * 6" {
		t.Errorf("expected custom full cron '30 1 * * 6', got '%s'", expr)
	}
	if expr := getIncrCronExpr(bkCustom); expr != "15 4 * * *" {
		t.Errorf("expected custom incr cron '15 4 * * *', got '%s'", expr)
	}
}

func TestRunBackupList(t *testing.T) {
	oldRootCheck := ensureRootFunc
	ensureRootFunc = func() {}
	defer func() { ensureRootFunc = oldRootCheck }()

	// Mock config
	tempDir, err := os.MkdirTemp("", "pg_mgr_backup_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() {
		config.ConfigFilePath = "/etc/pg_mgr/conf.yaml"
	}()

	config.Global.Instances = map[string]config.InstanceMeta{
		"inst1": {
			User:    "postgres",
			DataDir: filepath.Join(tempDir, "inst1"),
			Port:    "5432",
			Pgrman: &config.PgrmanConfig{
				Tool:           "pgrman",
				BackupDir:      filepath.Join(tempDir, "backup/inst1"),
				FullBackupCron: "0 2 * * 0",
				IncrBackupCron: "0 3 * * *",
			},
		},
		"inst2": {
			User:    "postgres",
			DataDir: filepath.Join(tempDir, "inst2"),
			Port:    "5433",
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runBackupList()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "inst1") {
		t.Errorf("expected output to contain 'inst1', got:\n%s", output)
	}
	if !strings.Contains(output, "pg_rman") {
		t.Errorf("expected output to contain 'pg_rman', got:\n%s", output)
	}
	if !strings.Contains(output, "inst2") {
		t.Errorf("expected output to contain 'inst2', got:\n%s", output)
	}
}

func TestRunPgrmanEditFlags(t *testing.T) {
	oldRootCheck := ensureRootFunc
	ensureRootFunc = func() {}
	defer func() { ensureRootFunc = oldRootCheck }()

	tempDir, err := os.MkdirTemp("", "pg_mgr_edit_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() {
		config.ConfigFilePath = "/etc/pg_mgr/conf.yaml"
	}()

	oldBackupDir := filepath.Join(tempDir, "backup/inst1")
	newBackupDir := filepath.Join(tempDir, "backup/inst1_new")
	arcLogDir := filepath.Join(tempDir, "archive/inst1")

	currentOSUser := "root"
	if u, err := user.Current(); err == nil && u.Username != "" {
		currentOSUser = u.Username
	}

	config.Global.Instances = map[string]config.InstanceMeta{
		"inst1": {
			User:    currentOSUser,
			DataDir: filepath.Join(tempDir, "inst1"),
			Port:    "5432",
			Pgrman: &config.PgrmanConfig{
				Tool:           "pgrman",
				BackupDir:      oldBackupDir,
				SrvLogPath:     filepath.Join(tempDir, "inst1/log"),
				ArcLogPath:     arcLogDir,
				CompressData:   "YES",
				KeepArcLogDays: 7,
				KeepSrvLogDays: 14,
				KeepDataDays:   14,
				FullBackupCron: "0 2 * * 0",
				IncrBackupCron: "0 3 * * *",
			},
		},
	}

	pgrmanInstance = "inst1"
	pgrmanEditBackupDir = newBackupDir
	pgrmanEditFullCron = "0 4 * * 0"
	pgrmanEditIncrCron = "0 5 * * *"

	cmd := pgrmanEditCmd
	_ = cmd.Flags().Set("instance", "inst1")
	_ = cmd.Flags().Set("backup-dir", newBackupDir)
	_ = cmd.Flags().Set("full-cron", "0 4 * * 0")
	_ = cmd.Flags().Set("incr-cron", "0 5 * * *")

	runPgrmanEdit(cmd)

	updatedInst := config.Global.Instances["inst1"]
	if updatedInst.Pgrman == nil {
		t.Fatalf("expected pgrman config to be present")
	}
	if updatedInst.Pgrman.BackupDir != newBackupDir {
		t.Errorf("expected BackupDir to be %s, got %s", newBackupDir, updatedInst.Pgrman.BackupDir)
	}
	if updatedInst.Pgrman.FullBackupCron != "0 4 * * 0" {
		t.Errorf("expected FullBackupCron to be '0 4 * * 0', got %s", updatedInst.Pgrman.FullBackupCron)
	}
	if updatedInst.Pgrman.IncrBackupCron != "0 5 * * *" {
		t.Errorf("expected IncrBackupCron to be '0 5 * * *', got %s", updatedInst.Pgrman.IncrBackupCron)
	}

	iniPath := filepath.Join(newBackupDir, "pg_rman.ini")
	if _, err := os.Stat(iniPath); err != nil {
		t.Errorf("expected pg_rman.ini file to exist at %s", iniPath)
	}
}

func TestRunPgrmanEditMigration(t *testing.T) {
	oldRootCheck := ensureRootFunc
	ensureRootFunc = func() {}
	defer func() { ensureRootFunc = oldRootCheck }()

	tempDir, err := os.MkdirTemp("", "pg_mgr_edit_mig_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "conf.yaml")
	config.ConfigFilePath = configPath
	defer func() { config.ConfigFilePath = "/etc/pg_mgr/conf.yaml" }()

	oldBackupDir := filepath.Join(tempDir, "old_backup")
	newBackupDir := filepath.Join(tempDir, "new_backup")
	_ = os.MkdirAll(oldBackupDir, 0755)
	_ = os.WriteFile(filepath.Join(oldBackupDir, "backup.ini"), []byte("backup_mode=FULL"), 0644)

	currentOSUser := "root"
	if u, err := user.Current(); err == nil && u.Username != "" {
		currentOSUser = u.Username
	}

	config.Global.Instances = map[string]config.InstanceMeta{
		"inst1": {
			User:    currentOSUser,
			DataDir: filepath.Join(tempDir, "inst1"),
			Port:    "5432",
			Pgrman: &config.PgrmanConfig{
				Tool:      "pgrman",
				BackupDir: oldBackupDir,
			},
		},
	}

	pgrmanInstance = "inst1"
	pgrmanEditBackupDir = newBackupDir
	pgrmanEditMigrate = true

	cmd := pgrmanEditCmd
	_ = cmd.Flags().Set("instance", "inst1")
	_ = cmd.Flags().Set("backup-dir", newBackupDir)
	_ = cmd.Flags().Set("migrate", "true")

	defer func() {
		pgrmanInstance = ""
		pgrmanEditBackupDir = ""
		pgrmanEditMigrate = false
	}()

	runPgrmanEdit(cmd)

	if _, err := os.Stat(oldBackupDir); !os.IsNotExist(err) {
		t.Errorf("expected oldBackupDir to be migrated/removed")
	}

	migratedFile := filepath.Join(newBackupDir, "backup.ini")
	if content, err := os.ReadFile(migratedFile); err != nil || string(content) != "backup_mode=FULL" {
		t.Errorf("expected backup.ini to be migrated to new backup directory")
	}
}


