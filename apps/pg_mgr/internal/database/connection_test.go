package database

import (
	"errors"
	"path/filepath"
	"testing"

	"pg_mgr/internal/config"
)

func TestResolveSuperuserUsesAndPersistsPostgresForLegacyInstance(t *testing.T) {
	oldProbe := probeConnection
	probeConnection = func(config.InstanceMeta, string, string) error { return nil }
	defer func() { probeConnection = oldProbe }()

	oldPath := config.ConfigFilePath
	oldGlobal := config.Global
	defer func() {
		config.ConfigFilePath = oldPath
		config.Global = oldGlobal
	}()
	config.ConfigFilePath = filepath.Join(t.TempDir(), "conf.yaml")
	config.Global = config.GlobalConfig{
		BaseDir:   "/pg",
		Instances: map[string]config.InstanceMeta{"legacy": {User: "alice", Port: "5432"}},
	}

	dbUser, err := ResolveSuperuser("legacy", config.Global.Instances["legacy"], false)
	if err != nil {
		t.Fatal(err)
	}
	if dbUser != "postgres" {
		t.Fatalf("dbUser = %q, want postgres", dbUser)
	}
	if got := config.Global.Instances["legacy"].DatabaseUser; got != "postgres" {
		t.Fatalf("persisted database user = %q, want postgres", got)
	}
	if got := config.Global.Instances["legacy"].DatabaseName; got != "postgres" {
		t.Fatalf("persisted database name = %q, want postgres", got)
	}
}

func TestResolveSuperuserReturnsConfiguredRoleWithoutProbe(t *testing.T) {
	oldProbe := probeConnection
	probeConnection = func(config.InstanceMeta, string, string) error {
		return errors.New("probe must not run")
	}
	defer func() { probeConnection = oldProbe }()

	connection, err := Resolve("configured", config.InstanceMeta{DatabaseUser: "dbadmin", DatabaseName: "appdb"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if connection.User != "dbadmin" || connection.Database != "appdb" {
		t.Fatalf("connection = %+v", connection)
	}
}
