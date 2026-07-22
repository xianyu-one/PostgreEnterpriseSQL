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
