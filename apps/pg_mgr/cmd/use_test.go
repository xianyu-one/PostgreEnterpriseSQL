package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pg_mgr/internal/config"
)

func TestUpdateProfileFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bashrc_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bashrcPath := filepath.Join(tmpDir, ".bashrc")

	// Case 1: Empty file
	err = updateProfileFile(bashrcPath, "/app/postgresql")
	if err != nil {
		t.Fatalf("updateProfileFile failed: %v", err)
	}

	contentBytes, err := os.ReadFile(bashrcPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content := string(contentBytes)
	if !strings.Contains(content, "# >>> pg_mgr sbin path >>>") || !strings.Contains(content, `export PG_MGR_BASE_DIR="/app/postgresql"`) {
		t.Errorf("incorrect profile content: %s", content)
	}

	// Case 2: Replace existing block
	err = updateProfileFile(bashrcPath, "/app/custom_postgresql")
	if err != nil {
		t.Fatalf("updateProfileFile failed on replace: %v", err)
	}

	contentBytes, err = os.ReadFile(bashrcPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content = string(contentBytes)
	if !strings.Contains(content, `export PG_MGR_BASE_DIR="/app/custom_postgresql"`) {
		t.Errorf("expected new base dir in profile: %s", content)
	}
	if strings.Contains(content, `export PG_MGR_BASE_DIR="/app/postgresql"`) {
		t.Errorf("old base dir still exists: %s", content)
	}

	// Case 3: Preserving other content
	preexisting := "some preexisting command\n"
	err = os.WriteFile(bashrcPath, []byte(preexisting+content), 0644)
	if err != nil {
		t.Fatalf("failed to write preexisting content: %v", err)
	}

	err = updateProfileFile(bashrcPath, "/app/final_postgresql")
	if err != nil {
		t.Fatalf("updateProfileFile failed on replace with preexisting content: %v", err)
	}

	contentBytes, err = os.ReadFile(bashrcPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	content = string(contentBytes)
	if !strings.HasPrefix(content, preexisting) {
		t.Errorf("expected preexisting content to be preserved, got: %s", content)
	}
	if !strings.Contains(content, `export PG_MGR_BASE_DIR="/app/final_postgresql"`) {
		t.Errorf("expected new final base dir in profile: %s", content)
	}
}

func TestRunUseCommand(t *testing.T) {
	// Setup mock instances registry
	origInstances := config.Global.Instances
	origBaseDir := config.Global.BaseDir
	defer func() {
		config.Global.Instances = origInstances
		config.Global.BaseDir = origBaseDir
	}()

	config.Global.BaseDir = "/app/postgresql"
	config.Global.Instances = map[string]config.InstanceMeta{
		"test_inst": {
			User:    "nobody",
			DataDir: "/app/postgresql/data_test_inst",
			BinPath: "/app/postgresql/16/9/bin/postgres",
			Port:    "5432",
		},
	}

	// Capture stdout and stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	// Run the use command
	runUse("test_inst")

	wOut.Close()
	wErr.Close()

	var bufOut bytes.Buffer
	io.Copy(&bufOut, rOut)
	var bufErr bytes.Buffer
	io.Copy(&bufErr, rErr)

	stdoutStr := bufOut.String()
	stderrStr := bufErr.String()

	// Verify stdout contains the correct export commands
	expectedStdoutLines := []string{
		"export PG_VERSION_PATH='/app/postgresql/16/9'",
		"export PG_RMAN_BACK_PATH='/app/postgresql/backup_test_inst'",
		"export PATH='/app/postgresql/16/9/bin':$PATH",
		"export PGDATA='/app/postgresql/data_test_inst'",
		"export LD_LIBRARY_PATH=':/app/postgresql/16/9/lib/'",
		"export PGPORT='5432'",
	}

	for _, line := range expectedStdoutLines {
		if !strings.Contains(stdoutStr, line) {
			t.Errorf("stdout missing: %q\nGot:\n%s", line, stdoutStr)
		}
	}

	// Stderr should contain instructions/warnings but not stdout commands
	if !strings.Contains(stderrStr, "Run 'eval $(pg_mgr use <instance_name>)'") {
		t.Errorf("stderr missing run instructions\nGot:\n%s", stderrStr)
	}
}
