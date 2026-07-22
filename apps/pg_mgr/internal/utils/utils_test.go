package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	cmd1 := BuildArchiveCommand("", "cp %p /arc/%f")
	user1, pgMgr1 := ParseArchiveCommand(cmd1)
	if user1 != "" {
		t.Errorf("expected empty userPart, got: '%s'", user1)
	}
	if pgMgr1 != "cp %p /arc/%f" {
		t.Errorf("expected pgMgrPart 'cp %%p /arc/%%f', got: '%s'", pgMgr1)
	}

	// Case 2: Preserve user command when setting pg_mgr command
	userCmd := "test ! -f /user/arc/%f && cp %p /user/arc/%f"
	cmd2 := BuildArchiveCommand(userCmd, "cp %p /pgmgr/arc/%f")
	user2, pgMgr2 := ParseArchiveCommand(cmd2)
	if user2 != userCmd {
		t.Errorf("expected userPart '%s', got: '%s'", userCmd, user2)
	}
	if pgMgr2 != "cp %p /pgmgr/arc/%f" {
		t.Errorf("expected pgMgrPart 'cp %%p /pgmgr/arc/%%f', got: '%s'", pgMgr2)
	}

	// Case 3: Update pg_mgr command preserving user command
	cmd3 := BuildArchiveCommand(user2, "cp %p /new/pgmgr/arc/%f")
	user3, pgMgr3 := ParseArchiveCommand(cmd3)
	if user3 != userCmd {
		t.Errorf("expected userPart '%s', got: '%s'", userCmd, user3)
	}
	if pgMgr3 != "cp %p /new/pgmgr/arc/%f" {
		t.Errorf("expected pgMgrPart 'cp %%p /new/pgmgr/arc/%%f', got: '%s'", pgMgr3)
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

