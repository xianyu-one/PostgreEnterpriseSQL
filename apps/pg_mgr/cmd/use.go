package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var useCmd = &cobra.Command{
	Use:     "use [instance_name]",
	Aliases: []string{"switch"},
	Short:   i18n.T("use_desc"),
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 1 {
			runUse(args[0])
			return
		}
		selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		runUse(selected)
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

func runUse(instanceName string) {
	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		fmt.Fprintf(os.Stderr, i18n.T("err_not_reg")+"\n", instanceName)
		os.Exit(1)
	}

	versionPathFull := filepath.Dir(filepath.Dir(meta.BinPath))
	backupDir := filepath.Join(config.Global.BaseDir, fmt.Sprintf("backup_%s", instanceName))

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
		}

		if err := utils.UpdatePgrc(pgrcPath, envs); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update %s: %v\n", pgrcPath, err)
		} else {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			_ = os.Chown(pgrcPath, uid, gid)
			fmt.Fprintf(os.Stderr, "Updated %s environment configuration.\n", pgrcPath)
		}
	}

	// Print exports to stdout for eval
	fmt.Printf("export PG_VERSION_PATH='%s'\n", versionPathFull)
	fmt.Printf("export PG_RMAN_BACK_PATH='%s'\n", backupDir)
	fmt.Printf("export PATH='%s/bin':$PATH\n", versionPathFull)
	fmt.Printf("export PGDATA='%s'\n", meta.DataDir)
	fmt.Printf("export LD_LIBRARY_PATH=':%s/lib/'\n", versionPathFull)
	fmt.Printf("export PGPORT='%s'\n", meta.Port)
	fmt.Fprintln(os.Stderr, "Run 'eval $(pg_mgr use <instance_name>)' to switch the current shell environment.")
}
