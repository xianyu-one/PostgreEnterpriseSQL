package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var useCmd = &cobra.Command{
	Use:     "use [instance_name]",
	Aliases: []string{"switch"},
	Short:   i18n.T("use_desc"),
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return runUse(args[0])
		}
		if UI.NonInteractive {
			return interaction.MissingFlags("instance_name")
		}
		selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
		if err != nil {
			return err
		}
		return runUse(selected)
	},
}

func init() {
	useCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var list []string
		for name := range config.Global.Instances {
			list = append(list, name)
		}
		return list, cobra.ShellCompDirectiveNoFileComp
	}
	InstanceCmd.AddCommand(useCmd)
	RootCmd.AddCommand(useCmd)
}

func runUse(instanceName string) error {
	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_not_reg", instanceName), interaction.ExitTarget).WithDetail("instance", instanceName)
	}

	versionPathFull := filepath.Dir(filepath.Dir(meta.BinPath))
	backupDir := filepath.Join(config.Global.BaseDir, fmt.Sprintf("backup_%s", instanceName))
	if meta.Pgrman != nil && strings.TrimSpace(meta.Pgrman.BackupDir) != "" {
		backupDir = filepath.Clean(meta.Pgrman.BackupDir)
	}
	databaseUser := meta.DatabaseUser
	if databaseUser == "" {
		databaseUser = "postgres"
	}
	databaseName := meta.DatabaseName
	if databaseName == "" {
		databaseName = "postgres"
	}

	// Write to .pgrc of the instance's OS user
	u, err := user.Lookup(meta.User)
	if err != nil {
		fmt.Fprintf(os.Stderr, i18n.T("err_user_not_found")+"\n", meta.User)
	} else {
		pgrcPath := filepath.Join(u.HomeDir, ".pgrc")
		envs := map[string]string{
			"PG_VERSION_PATH":   fmt.Sprintf("'%s'", versionPathFull),
			"PG_RMAN_BACK_PATH": fmt.Sprintf("'%s'", backupDir),
			"PATH":              fmt.Sprintf("'%s/bin':$PATH", versionPathFull),
			"PGDATA":            fmt.Sprintf("'%s'", meta.DataDir),
			"LD_LIBRARY_PATH":   fmt.Sprintf("':%s/lib/'", versionPathFull),
			"PGPORT":            fmt.Sprintf("'%s'", meta.Port),
			"PGUSER":            fmt.Sprintf("'%s'", databaseUser),
			"PGDATABASE":        fmt.Sprintf("'%s'", databaseName),
		}

		if err := utils.UpdatePgrc(pgrcPath, envs); err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("warn_profile_update", pgrcPath, err))
		} else if err := ensurePgMgrUseShellIntegration(pgrcPath); err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("warn_profile_update", pgrcPath, err))
		} else {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			_ = os.Chown(pgrcPath, uid, gid)
			fmt.Fprintln(os.Stderr, i18n.T("profile_updated", pgrcPath))
		}
	}

	// Print exports to stdout for eval
	fmt.Printf("export PG_VERSION_PATH='%s'\n", versionPathFull)
	fmt.Printf("export PG_RMAN_BACK_PATH='%s'\n", backupDir)
	fmt.Printf("export PATH='%s/bin':$PATH\n", versionPathFull)
	fmt.Printf("export PGDATA='%s'\n", meta.DataDir)
	fmt.Printf("export LD_LIBRARY_PATH=':%s/lib/'\n", versionPathFull)
	fmt.Printf("export PGPORT='%s'\n", meta.Port)
	fmt.Printf("export PGUSER='%s'\n", databaseUser)
	fmt.Printf("export PGDATABASE='%s'\n", databaseName)
	fmt.Fprintln(os.Stderr, i18n.T("use_guidance"))
	return nil
}

func ensurePgMgrUseShellIntegration(pgrcPath string) error {
	const marker = "PG_MGR_USE_SHELL_INTEGRATION_START"
	content, err := os.ReadFile(pgrcPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(content), marker) {
		return nil
	}
	const integration = `
# PG_MGR_USE_SHELL_INTEGRATION_START
pg_mgr() {
    if [ "$1" = "use" ] || { [ "$1" = "instance" ] && [ "$2" = "use" ]; }; then
        eval "$(command pg_mgr "$@")"
    else
        command pg_mgr "$@"
    fi
}
# PG_MGR_USE_SHELL_INTEGRATION_END
`
	return utils.AppendToFile(pgrcPath, integration)
}
