package utils

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUserSystemctlProvidesUserRuntimeDirectory(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	outputPath := filepath.Join(t.TempDir(), "systemctl-env")
	stub := filepath.Join(binDir, "systemctl")
	stubContent := "#!/bin/sh\nprintf '%s\\n' \"$XDG_RUNTIME_DIR\" \"$*\" > \"$PG_MGR_SYSTEMCTL_TEST_OUTPUT\"\n"
	if err := os.WriteFile(stub, []byte(stubContent), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PG_MGR_SYSTEMCTL_TEST_OUTPUT", outputPath)
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/wrong")

	if err := RunUserSystemctl(current.Username, "daemon-reload"); err != nil {
		t.Fatalf("RunUserSystemctl failed: %v", err)
	}

	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/run/user", current.Uid) + "\n--user daemon-reload"
	if got := strings.TrimSpace(string(output)); got != want {
		t.Fatalf("systemctl environment and arguments = %q, want %q", got, want)
	}
}
