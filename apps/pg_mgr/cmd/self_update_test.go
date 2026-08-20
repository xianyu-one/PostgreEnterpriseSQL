package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceExecutableAtomically(t *testing.T) {
	dir := t.TempDir()
	target := writeFakePgMgr(t, dir, "current", "old")
	candidate := writeFakePgMgr(t, dir, "candidate", "new")
	restarted := false

	if err := replaceExecutable(candidate, target, func() error {
		restarted = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !restarted {
		t.Fatal("daemon restart was not attempted")
	}
	assertFileContains(t, target, "new")
}

func TestReplaceExecutableRestoresPreviousBinaryWhenRestartFails(t *testing.T) {
	dir := t.TempDir()
	target := writeFakePgMgr(t, dir, "current", "old")
	candidate := writeFakePgMgr(t, dir, "candidate", "new")
	restarts := 0

	err := replaceExecutable(candidate, target, func() error {
		restarts++
		if restarts == 1 {
			return errors.New("new daemon failed")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "new daemon failed") {
		t.Fatalf("error = %v, want restart failure", err)
	}
	if restarts != 2 {
		t.Fatalf("restart attempts = %d, want 2", restarts)
	}
	assertFileContains(t, target, "old")
}

func TestValidatePgMgrBinaryRejectsUnrelatedExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho unrelated\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := validatePgMgrBinary(path); err == nil {
		t.Fatal("expected unrelated executable to be rejected")
	}
}

func TestValidatePgMgrBinaryReturnsVersion(t *testing.T) {
	path := writeFakePgMgr(t, t.TempDir(), "candidate", "v2.4.0")
	version, err := validatePgMgrBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	if version != "v2.4.0" {
		t.Fatalf("version = %q, want v2.4.0", version)
	}
}

func writeFakePgMgr(t *testing.T, dir, name, marker string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'pg_mgr version " + marker + "'; fi\n"
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), want) {
		t.Fatalf("%s does not contain %q", path, want)
	}
}
