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

func TestInstallCommandsDoNotDisruptLogind(t *testing.T) {
	t.Parallel()

	files := []string{"install_pkg.go", "install.go", "create.go"}
	for _, name := range files {
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		source := string(content)
		for _, forbidden := range []string{
			"restart\", \"systemd-logind",
		} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s contains session-disrupting logind operation %q", name, forbidden)
			}
		}
	}
}

func TestCreateCommandsUsePostgresDatabaseIdentity(t *testing.T) {
	initCmd := buildInitDBCommand(
		"/opt/postgresql/16/9",
		"/opt/postgresql/16/9/bin/pg_ctl",
		"/var/lib/postgresql/example",
		"postgres",
	)
	if !strings.Contains(initCmd, `initdb -o "--username=postgres"`) {
		t.Fatalf("initdb command must create the postgres database superuser: %s", initCmd)
	}

	passwordCmd := buildInitialPasswordCommand(
		"/opt/postgresql/16/9",
		"/opt/postgresql/16/9/bin/psql",
		51721,
		"postgres",
		"secret",
	)
	if !strings.Contains(passwordCmd, " -d postgres ") {
		t.Fatalf("password command must explicitly connect to the postgres database: %s", passwordCmd)
	}
	if !strings.Contains(passwordCmd, "ALTER USER postgres") {
		t.Fatalf("password command must update the postgres role: %s", passwordCmd)
	}
}

func TestCreateEnvironmentPreservesExistingProfileAndAliasIsOptIn(t *testing.T) {
	tempDir := t.TempDir()
	profilePath := filepath.Join(tempDir, ".bash_profile")
	pgrcPath := filepath.Join(tempDir, ".pgrc")
	original := "export EXISTING_SETTING=keep-me\n"
	if err := os.WriteFile(profilePath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureProfileSourcesPgrc(profilePath, pgrcPath); err != nil {
		t.Fatal(err)
	}
	if err := ensureProfileSourcesPgrc(profilePath, pgrcPath); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), original) {
		t.Fatalf("existing profile content was not preserved: %s", content)
	}
	if count := strings.Count(string(content), "source "+pgrcPath); count != 1 {
		t.Fatalf(".pgrc source line count = %d, want 1: %s", count, content)
	}

	if _, err := os.Stat(pgrcPath); !os.IsNotExist(err) {
		t.Fatalf("systemctl alias must not be added unless explicitly requested")
	}
	if err := ensureSystemctlAlias(pgrcPath); err != nil {
		t.Fatal(err)
	}
	aliasContent, err := os.ReadFile(pgrcPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(aliasContent), "alias systemctl='systemctl --user'") {
		t.Fatalf("requested systemctl alias was not added: %s", aliasContent)
	}
}

func TestProfileFileForShellSupportsBashAndZsh(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{shell: "/bin/bash", want: ".bash_profile"},
		{shell: "/usr/bin/zsh", want: ".zshrc"},
		{shell: "/bin/sh", want: ".bash_profile"},
		{shell: "", want: ".bash_profile"},
	}
	for _, tt := range tests {
		if got := profileFileForShell(tt.shell); got != tt.want {
			t.Errorf("profileFileForShell(%q) = %q, want %q", tt.shell, got, tt.want)
		}
	}
}

func TestCreateEnvironmentPreservesExistingZshrc(t *testing.T) {
	tempDir := t.TempDir()
	profilePath := filepath.Join(tempDir, profileFileForShell("/usr/bin/zsh"))
	pgrcPath := filepath.Join(tempDir, ".pgrc")
	original := "export ZSH_SETTING=keep-me\n"
	if err := os.WriteFile(profilePath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureProfileSourcesPgrc(profilePath, pgrcPath); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), original) ||
		!strings.Contains(string(content), "source "+pgrcPath) {
		t.Fatalf("zsh profile was not preserved and updated correctly: %s", content)
	}
}

func TestCreateCommandsSupportCustomDatabaseSuperuser(t *testing.T) {
	initCmd := buildInitDBCommand("/pg", "/pg/bin/pg_ctl", "/data", "dbadmin")
	if !strings.Contains(initCmd, "--username=dbadmin") {
		t.Fatalf("custom database superuser missing from initdb command: %s", initCmd)
	}
	passwordCmd := buildInitialPasswordCommand("/pg", "/pg/bin/psql", 5432, "dbadmin", "secret")
	if !strings.Contains(passwordCmd, "ALTER USER dbadmin") {
		t.Fatalf("custom database superuser missing from password command: %s", passwordCmd)
	}
}
