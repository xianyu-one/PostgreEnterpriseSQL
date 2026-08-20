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
	"pg_mgr/internal/database"
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

func TestBackupScheduleEnabledIsBackwardCompatible(t *testing.T) {
	if !isBackupScheduleEnabled(&config.PgrmanConfig{}) {
		t.Fatal("legacy configuration without schedule_enabled must remain enabled")
	}
	disabled := false
	if isBackupScheduleEnabled(&config.PgrmanConfig{ScheduleEnabled: &disabled}) {
		t.Fatal("explicitly disabled schedule must remain disabled")
	}
}

func TestBackupCatalogNeedsInit(t *testing.T) {
	tempDir := t.TempDir()
	oldDir := filepath.Join(tempDir, "old")
	if needsInit, err := backupCatalogNeedsInit(oldDir, oldDir); err != nil || !needsInit {
		t.Fatalf("missing unchanged catalog should be initialized: needsInit=%v err=%v", needsInit, err)
	}

	newDir := filepath.Join(tempDir, "new")
	if needsInit, err := backupCatalogNeedsInit(oldDir, newDir); err != nil || !needsInit {
		t.Fatalf("missing new catalog should be initialized: needsInit=%v err=%v", needsInit, err)
	}
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "system_identifier"), []byte("catalog"), 0644); err != nil {
		t.Fatal(err)
	}
	if needsInit, err := backupCatalogNeedsInit(oldDir, newDir); err != nil || needsInit {
		t.Fatalf("non-empty migrated catalog should not be initialized: needsInit=%v err=%v", needsInit, err)
	}
}

func TestEnsurePgrmanCatalogReadyInitializesMissingCatalog(t *testing.T) {
	backupDir := t.TempDir()
	meta := config.InstanceMeta{
		User:    "nobody",
		DataDir: "/data/instance",
		BinPath: "/pg/bin/postgres",
		Pgrman: &config.PgrmanConfig{
			Tool: "pgrman", BackupDir: backupDir, SrvLogPath: "/data/log", ArcLogPath: "/data/archive", CompressData: "YES",
		},
	}
	original := runPgrmanInitForRun
	t.Cleanup(func() { runPgrmanInitForRun = original })
	called := false
	runPgrmanInitForRun = func(got config.InstanceMeta, command string) ([]byte, error) {
		called = true
		if !strings.Contains(command, " init ") || !strings.Contains(command, backupDir) {
			t.Fatalf("unexpected init command: %s", command)
		}
		return nil, os.WriteFile(filepath.Join(backupDir, "system_identifier"), []byte("123"), 0644)
	}
	if err := ensurePgrmanCatalogReady(meta); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("pg_rman init was not called")
	}
	content, err := os.ReadFile(filepath.Join(backupDir, "pg_rman.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "SRVLOG_PATH='/data/log'") {
		t.Fatalf("recreated pg_rman.ini = %s", content)
	}
}

func TestBackupCatalogNeedsInitWhenConfiguredCatalogLacksSystemIdentifier(t *testing.T) {
	backupDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(backupDir, "pg_rman.ini"), []byte("SRVLOG_PATH='/data/log'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	needsInit, err := backupCatalogNeedsInit(backupDir, backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if !needsInit {
		t.Fatal("catalog without system_identifier must be reinitialized before backup")
	}
}

func TestValidatePgrmanBackupDate(t *testing.T) {
	validDates := []string{
		"2026-07-25 09:08:07",
		"2024-02-29 23:59:59",
	}
	for _, date := range validDates {
		if err := validatePgrmanBackupDate(date); err != nil {
			t.Errorf("expected %q to be valid, got %v", date, err)
		}
	}

	invalidDates := []string{
		"2026-07-25",
		"2026/07/25 09:08:07",
		"2026-02-29 09:08:07",
		"2026-07-25 24:00:00",
		"2026-07-25 09:08:07 extra",
	}
	for _, date := range invalidDates {
		if err := validatePgrmanBackupDate(date); err == nil {
			t.Errorf("expected %q to be invalid", date)
		}
	}
}

func TestBuildPgrmanDeleteCommand(t *testing.T) {
	meta := config.InstanceMeta{
		DataDir: "/data/instance one",
		Pgrman: &config.PgrmanConfig{
			Tool:      "pgrman",
			BackupDir: "/backup/instance one's",
		},
	}

	got := buildPgrmanDeleteCommand(meta, "2026-07-25 09:08:07")
	want := "'pg_rman' delete '2026-07-25 09:08:07' -B '/backup/instance one'\"'\"'s' -D '/data/instance one'"
	if got != want {
		t.Errorf("unexpected delete command:\nwant: %s\n got: %s", want, got)
	}
}

func TestBuildPgRmanBackupCommandUsesDatabaseUser(t *testing.T) {
	meta := config.InstanceMeta{
		Port:    "51721",
		DataDir: "/data/instance",
		Pgrman:  &config.PgrmanConfig{BackupDir: "/backup/instance", ArcLogPath: "/archive/instance", SrvLogPath: "/logs/instance"},
	}
	command := buildPgRmanBackupCommand(meta, "full", database.Connection{User: "dbadmin", Database: "appdb"})
	if !strings.Contains(command, "-U 'dbadmin'") {
		t.Fatalf("backup command does not specify database user: %s", command)
	}
	if !strings.Contains(command, "-d 'appdb'") {
		t.Fatalf("backup command does not specify database name: %s", command)
	}
	if !strings.Contains(command, "-A '/archive/instance'") {
		t.Fatalf("backup command does not specify archive log path: %s", command)
	}
	if !strings.Contains(command, "-S '/logs/instance'") {
		t.Fatalf("backup command does not specify server log path: %s", command)
	}
}

func TestRecoverPgrmanConfigFromCatalogRestoresMissingPaths(t *testing.T) {
	backupDir := t.TempDir()
	ini := "SRVLOG_PATH='/data/log'\nARCLOG_PATH='/data/archive'\nCOMPRESS_DATA=YES\nKEEP_ARCLOG_DAYS=7\nKEEP_SRVLOG_DAYS=14\nKEEP_DATA_DAYS=21\n"
	if err := os.WriteFile(filepath.Join(backupDir, "pg_rman.ini"), []byte(ini), 0644); err != nil {
		t.Fatal(err)
	}
	meta := config.InstanceMeta{Pgrman: &config.PgrmanConfig{Tool: "pgrman", BackupDir: backupDir}}
	recovered, changed, err := recoverPgrmanConfigFromCatalog(meta)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected missing backup configuration to be recovered")
	}
	if recovered.Pgrman.SrvLogPath != "/data/log" || recovered.Pgrman.ArcLogPath != "/data/archive" {
		t.Fatalf("recovered paths = %#v", recovered.Pgrman)
	}
	if recovered.Pgrman.KeepDataDays != 21 || recovered.Pgrman.CompressData != "YES" {
		t.Fatalf("recovered settings = %#v", recovered.Pgrman)
	}
}

func TestPgrmanArchiveLogPathFallsBackToManagedArchiveCommand(t *testing.T) {
	dataDir := t.TempDir()
	conf := "archive_command = 'true PG_MGR_ARCHIVE_START ; export PG_ARCHDIR=/archive/from-conf && cp %p $PG_ARCHDIR/%f && true PG_MGR_ARCHIVE_END'\n"
	if err := os.WriteFile(filepath.Join(dataDir, "postgresql.conf"), []byte(conf), 0644); err != nil {
		t.Fatal(err)
	}
	meta := config.InstanceMeta{DataDir: dataDir, Pgrman: &config.PgrmanConfig{BackupDir: "/backup"}}
	if got := pgrmanArchiveLogPath(meta); got != "/archive/from-conf" {
		t.Fatalf("pgrmanArchiveLogPath() = %q", got)
	}
}

func TestPgrmanServerLogPathFallsBackToDataLogDirectory(t *testing.T) {
	meta := config.InstanceMeta{DataDir: "/data/instance", Pgrman: &config.PgrmanConfig{BackupDir: "/backup"}}
	if got := pgrmanServerLogPath(meta); got != "/data/instance/log" {
		t.Fatalf("pgrmanServerLogPath() = %q", got)
	}
}

func TestPgrmanDeleteCommandRegistration(t *testing.T) {
	if pgrmanDeleteCmd.Parent() != pgrmanCmd {
		t.Fatal("expected delete command to be registered under backup pgrman")
	}
	if pgrmanDeleteCmd.Args == nil {
		t.Fatal("expected delete command to require a DATE argument")
	}
	if pgrmanDeleteCmd.Flag("instance") == nil {
		t.Fatal("expected delete command to provide --instance")
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
	oldInitRunner := runPgrmanInitForEdit
	runPgrmanInitForEdit = func(config.InstanceMeta, string) ([]byte, error) { return nil, nil }
	defer func() { runPgrmanInitForEdit = oldInitRunner }()

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
