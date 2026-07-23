package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsRootOrUser(t *testing.T) {
	currUser, err := GetCurrentOSUser()
	if err != nil {
		t.Fatalf("failed to get current OS user: %v", err)
	}

	// 1. Same user should return true
	if !IsRootOrUser(currUser) {
		t.Errorf("expected IsRootOrUser(%s) to return true", currUser)
	}

	// 2. Override root check to simulate root
	overrideTrue := true
	RootCheckOverride = &overrideTrue
	defer func() { RootCheckOverride = nil }()

	if !IsRootOrUser("some_other_user_12345") {
		t.Errorf("expected IsRootOrUser to return true when root override is set")
	}

	// 3. Override root check to simulate non-root
	overrideFalse := false
	RootCheckOverride = &overrideFalse

	if IsRootOrUser("non_existent_user_99999") {
		t.Errorf("expected IsRootOrUser to return false for non-matching user when non-root")
	}
}

func TestEnsurePkgPermissions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pg_mgr_pkg_perm_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}

	execPath := filepath.Join(binDir, "postgres")
	if err := os.WriteFile(execPath, []byte("mock binary"), 0700); err != nil {
		t.Fatalf("failed to write mock binary: %v", err)
	}

	docPath := filepath.Join(tempDir, "README")
	if err := os.WriteFile(docPath, []byte("mock doc"), 0600); err != nil {
		t.Fatalf("failed to write mock doc: %v", err)
	}

	if err := EnsurePkgPermissions(tempDir); err != nil {
		t.Fatalf("EnsurePkgPermissions failed: %v", err)
	}

	binInfo, err := os.Stat(binDir)
	if err != nil || binInfo.Mode().Perm() != 0755 {
		t.Errorf("expected binDir mode 0755, got %o", binInfo.Mode().Perm())
	}

	execInfo, err := os.Stat(execPath)
	if err != nil || execInfo.Mode().Perm() != 0755 {
		t.Errorf("expected execPath mode 0755, got %o", execInfo.Mode().Perm())
	}

	docInfo, err := os.Stat(docPath)
	if err != nil || docInfo.Mode().Perm() != 0644 {
		t.Errorf("expected docPath mode 0644, got %o", docInfo.Mode().Perm())
	}
}

func TestDetectDirOwner(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pg_mgr_owner_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	currUser, err := GetCurrentOSUser()
	if err != nil {
		t.Fatalf("failed to get current OS user: %v", err)
	}

	owner := DetectDirOwner(tempDir)
	if owner != currUser {
		t.Errorf("expected owner %s, got %s", currUser, owner)
	}
}
