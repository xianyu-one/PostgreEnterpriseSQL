package interaction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSecretFromEnvironmentOrProtectedFile(t *testing.T) {
	t.Setenv("PG_MGR_TEST_PASSWORD", "from-env")
	got, source, err := ResolveSecret("", "PG_MGR_TEST_PASSWORD", "")
	if err != nil || got != "from-env" || source != "environment" {
		t.Fatalf("environment result = %q, %q, %v", got, source, err)
	}

	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("from-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, source, err = ResolveSecret("", "", path)
	if err != nil || got != "from-file" || source != "protected file" {
		t.Fatalf("file result = %q, %q, %v", got, source, err)
	}
}

func TestResolveSecretRejectsAmbiguousSources(t *testing.T) {
	if _, _, err := ResolveSecret("literal", "ENV", "file"); err == nil {
		t.Fatal("expected multiple secret sources to fail")
	}
}
