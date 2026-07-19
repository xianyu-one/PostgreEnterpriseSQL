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
