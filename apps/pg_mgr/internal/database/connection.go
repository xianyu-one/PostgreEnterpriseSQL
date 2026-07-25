package database

import (
	"fmt"
	"path/filepath"
	"strings"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var probeConnection = probeDatabaseConnection

type Connection struct {
	User     string
	Database string
}

// ResolveSuperuser returns the configured database login role. For legacy
// instances it probes PostgreSQL's conventional "postgres" role, then asks the
// user when interactive recovery is allowed. A successfully resolved role is
// persisted in the instance registry.
func Resolve(instanceName string, meta config.InstanceMeta, interactive bool) (Connection, error) {
	if meta.DatabaseUser != "" && meta.DatabaseName != "" {
		return Connection{User: meta.DatabaseUser, Database: meta.DatabaseName}, nil
	}

	dbUser := meta.DatabaseUser
	if dbUser == "" {
		dbUser = "postgres"
	}
	databaseName := meta.DatabaseName
	if databaseName == "" {
		databaseName = "postgres"
	}
	if err := probeConnection(meta, dbUser, databaseName); err == nil {
		if err := config.SaveInstanceDatabaseConnection(instanceName, dbUser, databaseName); err != nil {
			return Connection{}, err
		}
		return Connection{User: dbUser, Database: databaseName}, nil
	}
	if !interactive {
		return Connection{}, fmt.Errorf("%s", i18n.T("err_db_connection_required", instanceName))
	}

	for {
		if meta.DatabaseUser == "" {
			dbUser = strings.TrimSpace(utils.PromptInput(i18n.T("prompt_db_user"), dbUser))
		}
		if meta.DatabaseName == "" {
			databaseName = strings.TrimSpace(utils.PromptInput(i18n.T("prompt_database_name"), databaseName))
		}
		if dbUser == "" || databaseName == "" {
			continue
		}
		if err := probeConnection(meta, dbUser, databaseName); err != nil {
			fmt.Println(i18n.T("err_db_login_failed", dbUser, databaseName, err))
			continue
		}
		if err := config.SaveInstanceDatabaseConnection(instanceName, dbUser, databaseName); err != nil {
			return Connection{}, err
		}
		return Connection{User: dbUser, Database: databaseName}, nil
	}
}

func ResolveSuperuser(instanceName string, meta config.InstanceMeta, interactive bool) (string, error) {
	connection, err := Resolve(instanceName, meta, interactive)
	return connection.User, err
}

func probeDatabaseConnection(meta config.InstanceMeta, dbUser, databaseName string) error {
	psql := filepath.Join(utils.GetInstanceBinDir(meta), "psql")
	command := fmt.Sprintf("%s -X -Aqt -p %s -d %s -U %s -c 'SELECT 1'",
		shellQuote(psql), shellQuote(meta.Port), shellQuote(databaseName), shellQuote(dbUser))
	output, err := utils.RunAsUserWithOutputForInstance(meta.User, meta, command)
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(output))
	}
	if strings.TrimSpace(output) != "1" {
		return fmt.Errorf("unexpected database response: %s", strings.TrimSpace(output))
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
