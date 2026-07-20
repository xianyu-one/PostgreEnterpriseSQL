package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"pg_mgr/internal/config"
)


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
