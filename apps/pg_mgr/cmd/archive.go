package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/database"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var (
	archiveInstance  string
	archiveDir       string
	archiveCommand   string
	archiveSilent    bool
	archiveMigrate   bool
	archiveCheckRoot = func() bool { return os.Geteuid() == 0 }
)

var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: i18n.T("archive_desc"),
}

var archiveStatusCmd = &cobra.Command{
	Use:     "show [instance_name]",
	Aliases: []string{"status"},
	Short:   i18n.T("archive_status_desc"),
	RunE: func(cmd *cobra.Command, args []string) error {
		inst := archiveInstance
		if len(args) > 0 {
			inst = args[0]
		}
		if UI.NonInteractive && inst == "" {
			return interaction.MissingFlags("instance_name or --instance")
		}
		return runArchiveStatus(inst)
	},
}

var archiveEnableCmd = &cobra.Command{
	Use:   "enable [instance_name]",
	Short: i18n.T("archive_enable_desc"),
	RunE: func(cmd *cobra.Command, args []string) error {
		inst := archiveInstance
		if len(args) > 0 {
			inst = args[0]
		}
		if UI.NonInteractive && inst == "" && !UI.LegacySilent {
			return interaction.MissingFlags("instance_name or --instance")
		}
		if UI.NonInteractive && archiveDir == "" && archiveCommand == "" {
			return interaction.MissingFlags("--dir or --command")
		}
		if UI.NonInteractive && !UI.Yes {
			return interaction.MissingFlags("--yes")
		}
		return runArchiveEnable(inst)
	},
}

var archiveDisableCmd = &cobra.Command{
	Use:   "disable [instance_name]",
	Short: i18n.T("archive_disable_desc"),
	RunE: func(cmd *cobra.Command, args []string) error {
		inst := archiveInstance
		if len(args) > 0 {
			inst = args[0]
		}
		if UI.NonInteractive && inst == "" && !UI.LegacySilent {
			return interaction.MissingFlags("instance_name or --instance")
		}
		if UI.NonInteractive && !UI.Yes {
			return interaction.MissingFlags("--yes")
		}
		return runArchiveDisable(inst)
	},
}

var archiveSetCmd = &cobra.Command{
	Use:     "set [instance_name]",
	Aliases: []string{"modify"},
	Short:   i18n.T("archive_set_desc"),
	RunE: func(cmd *cobra.Command, args []string) error {
		inst := archiveInstance
		if len(args) > 0 {
			inst = args[0]
		}
		if UI.NonInteractive && inst == "" && !UI.LegacySilent {
			return interaction.MissingFlags("instance_name or --instance")
		}
		if UI.NonInteractive && archiveDir == "" && archiveCommand == "" {
			return interaction.MissingFlags("--dir or --command")
		}
		if UI.NonInteractive && !UI.Yes {
			return interaction.MissingFlags("--yes")
		}
		return runArchiveEnable(inst)
	},
}

func init() {
	archiveCmd.PersistentFlags().StringVarP(&archiveInstance, "instance", "i", "", i18n.T("flag_instance"))
	archiveCmd.PersistentFlags().StringVarP(&archiveDir, "dir", "d", "", i18n.T("flag_archive_dir"))
	archiveCmd.PersistentFlags().StringVarP(&archiveCommand, "command", "c", "", i18n.T("flag_archive_command"))
	archiveCmd.PersistentFlags().BoolVarP(&archiveSilent, "silent", "s", false, i18n.T("flag_silent_deprecated"))
	_ = archiveCmd.PersistentFlags().MarkDeprecated("silent", i18n.T("flag_silent_replacement"))
	archiveCmd.PersistentFlags().BoolVarP(&archiveMigrate, "migrate", "m", false, i18n.T("flag_archive_migrate"))

	compFunc := func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var list []string
		for name := range config.Global.Instances {
			list = append(list, name)
		}
		return list, cobra.ShellCompDirectiveNoFileComp
	}

	archiveCmd.RegisterFlagCompletionFunc("instance", compFunc)

	archiveCmd.AddCommand(archiveStatusCmd)
	archiveCmd.AddCommand(archiveEnableCmd)
	archiveCmd.AddCommand(archiveDisableCmd)
	archiveCmd.AddCommand(archiveSetCmd)

	RootCmd.AddCommand(archiveCmd)
}

func runArchiveStatus(instanceName string) error {
	if instanceName == "" {
		if len(config.Global.Instances) == 0 {
			fmt.Println(i18n.T("err_no_instances"))
			return nil
		}
		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{
			i18n.T("tbl_inst"),
			i18n.T("tbl_archive_mode"),
			i18n.T("tbl_pgmgr_archive"),
		})

		for name, meta := range config.Global.Instances {
			confPath := filepath.Join(meta.DataDir, "postgresql.conf")
			arcMode, _ := utils.GetPostgresqlConfParam(confPath, "archive_mode")
			if arcMode == "" {
				arcMode = "off"
			}
			fullCmd, _ := utils.GetPostgresqlConfParam(confPath, "archive_command")
			_, pgMgrPart := utils.ParseArchiveCommand(fullCmd)
			if pgMgrPart == "" {
				pgMgrPart = "-"
			}

			t.AppendRow(table.Row{
				text.FgHiCyan.Sprint(name),
				arcMode,
				pgMgrPart,
			})
		}
		t.SetStyle(table.StyleLight)
		t.Render()
		return nil
	}

	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_inst_not_found", instanceName), interaction.ExitTarget).WithDetail("instance", instanceName)
	}

	confPath := filepath.Join(meta.DataDir, "postgresql.conf")
	arcMode, _ := utils.GetPostgresqlConfParam(confPath, "archive_mode")
	if arcMode == "" {
		arcMode = "off"
	}
	fullCmd, _ := utils.GetPostgresqlConfParam(confPath, "archive_command")
	userPart, pgMgrPart := utils.ParseArchiveCommand(fullCmd)
	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": instanceName, "archive_mode": arcMode, "archive_command": fullCmd, "pg_mgr_archive_command": pgMgrPart, "user_archive_command": userPart})
	}

	fmt.Printf("%s: %s\n", i18n.T("lbl_archive_mode"), text.FgHiCyan.Sprint(arcMode))
	fmt.Printf("%s: %s\n", i18n.T("lbl_archive_cmd"), fullCmd)
	fmt.Printf("%s: %s\n", i18n.T("lbl_pgmgr_archive_cmd"), pgMgrPart)
	fmt.Printf("%s: %s\n", i18n.T("lbl_user_archive_cmd"), userPart)
	return nil
}

func runArchiveEnable(instanceName string) error {
	if instanceName == "" {
		if !archiveSilent {
			selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
			if err != nil {
				fmt.Println(text.FgHiRed.Sprint(err))
				return err
			}
			instanceName = selected
		} else {
			instanceName = "default"
		}
	}

	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_inst_not_found", instanceName), interaction.ExitTarget).WithDetail("instance", instanceName)
	}

	if !archiveCheckRoot() && !utils.IsRootOrUser(meta.User) {
		return interaction.NewError(interaction.CodePermissionDenied, i18n.T("req_root_or_user", meta.User), interaction.ExitPermission).WithDetail("allowed_identities", []string{"root", meta.User})
	}

	confPath := filepath.Join(meta.DataDir, "postgresql.conf")
	oldFullCmd, _ := utils.GetPostgresqlConfParam(confPath, "archive_command")
	userPart, oldPgMgrCmd := utils.ParseArchiveCommand(oldFullCmd)
	oldArchiveDir := utils.ExtractArchiveDirFromCmd(oldPgMgrCmd)

	targetDir := ""
	newPgMgrCmd := archiveCommand
	if newPgMgrCmd == "" && archiveDir != "" {
		targetDir = filepath.Clean(archiveDir)
		newPgMgrCmd = managedArchiveCopyCommand(targetDir)
	}

	if newPgMgrCmd == "" && !archiveSilent {
		choice := utils.PromptInput("Select configuration mode [1: Directory, 2: Command]", "1")
		if choice == "1" {
			defaultDir := filepath.Join(config.Global.BaseDir, "archive", instanceName)
			dir := utils.PromptPath(i18n.T("prompt_archive_dir"), defaultDir)
			targetDir = filepath.Clean(dir)
			newPgMgrCmd = managedArchiveCopyCommand(targetDir)
		} else {
			defaultCmd := managedArchiveCopyCommand(filepath.Join(config.Global.BaseDir, "archive", instanceName))
			newPgMgrCmd = utils.PromptInput(i18n.T("prompt_archive_cmd"), defaultCmd)
			targetDir = utils.ExtractArchiveDirFromCmd(newPgMgrCmd)
		}
	}

	if targetDir == "" && newPgMgrCmd != "" {
		targetDir = utils.ExtractArchiveDirFromCmd(newPgMgrCmd)
	}

	if targetDir != "" {
		doMigrate := archiveMigrate
		if !doMigrate && !archiveSilent && oldArchiveDir != "" && oldArchiveDir != targetDir {
			if _, err := os.Stat(oldArchiveDir); err == nil {
				doMigrate = utils.PromptConfirm(i18n.T("prompt_migrate_archive", oldArchiveDir, targetDir))
			}
		}

		if oldArchiveDir != "" && targetDir != oldArchiveDir && doMigrate {
			fmt.Fprintln(os.Stderr, i18n.T("migrate_archive_start", oldArchiveDir, targetDir))
			if err := utils.MigrateDirectory(oldArchiveDir, targetDir); err != nil {
				return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_migrate_archive_failed", err), interaction.ExitExecution).WithCause(err)
			} else {
				fmt.Fprintln(os.Stderr, text.FgGreen.Sprint(i18n.T("migrate_archive_success", oldArchiveDir, targetDir)))
			}
		}

		u, err := user.Lookup(meta.User)
		if err == nil {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			os.MkdirAll(targetDir, 0755)
			os.Chown(targetDir, uid, gid)
		}

		if meta.Pgrman != nil {
			meta.Pgrman.ArcLogPath = targetDir
			_ = config.SaveInstancePgrmanConfig(instanceName, meta.Pgrman)

			if meta.Pgrman.BackupDir != "" {
				if _, err := os.Stat(meta.Pgrman.BackupDir); err == nil {
					iniPath := filepath.Join(meta.Pgrman.BackupDir, "pg_rman.ini")
					compressData := meta.Pgrman.CompressData
					if compressData == "" {
						compressData = "YES"
					}
					iniContent := fmt.Sprintf("SRVLOG_PATH='%s'\nARCLOG_PATH='%s'\nCOMPRESS_DATA=%s\nKEEP_ARCLOG_DAYS=%d\nKEEP_SRVLOG_DAYS=%d\nKEEP_DATA_DAYS=%d\n",
						meta.Pgrman.SrvLogPath, targetDir, compressData,
						meta.Pgrman.KeepArcLogDays, meta.Pgrman.KeepSrvLogDays, meta.Pgrman.KeepDataDays)
					if err := os.WriteFile(iniPath, []byte(iniContent), 0644); err == nil {
						if u, err := user.Lookup(meta.User); err == nil {
							uid, _ := strconv.Atoi(u.Uid)
							gid, _ := strconv.Atoi(u.Gid)
							_ = os.Chown(iniPath, uid, gid)
						}
					}
				}
			}
		}
	}

	if newPgMgrCmd == "" {
		return interaction.NewError(interaction.CodeMissingInput, i18n.T("err_archive_no_cmd"), interaction.ExitUsage)
	}

	newFullCmd := utils.BuildArchiveCommand(userPart, newPgMgrCmd)

	oldMode, _ := utils.GetPostgresqlConfParam(confPath, "archive_mode")

	if err := utils.UpdatePostgresqlConfParam(confPath, "archive_mode", "on"); err != nil {
		return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_failed", err), interaction.ExitExecution).WithCause(err)
	}
	if err := utils.UpdatePostgresqlConfParam(confPath, "archive_command", newFullCmd); err != nil {
		return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_failed", err), interaction.ExitExecution).WithCause(err)
	}

	var statusCmd string
	if meta.User == "root" {
		statusCmd = fmt.Sprintf("systemctl is-active postgresql-%s.service", instanceName)
	} else {
		statusCmd = fmt.Sprintf("systemctl --user is-active postgresql-%s.service", instanceName)
	}
	statusOut, _ := utils.RunAsUserWithOutput(meta.User, statusCmd)

	if statusOut == "active" {
		if oldMode == "off" || oldMode == "" {
			if !archiveSilent && utils.PromptConfirm("archive_mode requires a database restart to take effect. Restart now?") {
				stopOldService(instanceName, meta.User)
				startNewService(instanceName, meta.User)
				fmt.Fprintln(os.Stderr, text.FgGreen.Sprint(i18n.T("restart_success", instanceName)))
			} else {
				fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("warn_archive_enable_restart")))
			}
		} else {
			if meta.User == "root" {
				utils.RunCmd("systemctl", "reload", fmt.Sprintf("postgresql-%s.service", instanceName))
			} else {
				utils.RunAsUser(meta.User, fmt.Sprintf("systemctl --user reload postgresql-%s.service", instanceName))
			}
			fmt.Fprintln(os.Stderr, text.FgGreen.Sprint(i18n.T("postgres_reloaded")))
		}

		restartArchiverBackend(instanceName, meta)
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": instanceName, "archive_mode": "on", "archive_command": newFullCmd, "status": "configured"})
	}
	if !UI.Quiet {
		fmt.Println(text.FgGreen.Sprint(i18n.T("archive_enable_success", instanceName)))
	}
	fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("archive_check_notice", meta.Port)))
	return nil
}

func managedArchiveCopyCommand(targetDir string) string {
	return fmt.Sprintf("export PG_ARCHDIR=%s && (test -f $PG_ARCHDIR/%%f || cp %%p $PG_ARCHDIR/%%f)", targetDir)
}

func runArchiveDisable(instanceName string) error {
	if instanceName == "" {
		if !archiveSilent {
			selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
			if err != nil {
				fmt.Println(text.FgHiRed.Sprint(err))
				return err
			}
			instanceName = selected
		} else {
			instanceName = "default"
		}
	}

	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_inst_not_found", instanceName), interaction.ExitTarget).WithDetail("instance", instanceName)
	}

	if !archiveCheckRoot() && !utils.IsRootOrUser(meta.User) {
		return interaction.NewError(interaction.CodePermissionDenied, i18n.T("req_root_or_user", meta.User), interaction.ExitPermission).WithDetail("allowed_identities", []string{"root", meta.User})
	}

	confPath := filepath.Join(meta.DataDir, "postgresql.conf")
	oldFullCmd, _ := utils.GetPostgresqlConfParam(confPath, "archive_command")
	userPart, _ := utils.ParseArchiveCommand(oldFullCmd)

	newFullCmd := utils.BuildArchiveCommand(userPart, "")

	if err := utils.UpdatePostgresqlConfParam(confPath, "archive_command", newFullCmd); err != nil {
		return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_failed", err), interaction.ExitExecution).WithCause(err)
	}

	if userPart == "" {
		_ = utils.UpdatePostgresqlConfParam(confPath, "archive_mode", "off")
	}

	var statusCmd string
	if meta.User == "root" {
		statusCmd = fmt.Sprintf("systemctl is-active postgresql-%s.service", instanceName)
	} else {
		statusCmd = fmt.Sprintf("systemctl --user is-active postgresql-%s.service", instanceName)
	}
	statusOut, _ := utils.RunAsUserWithOutput(meta.User, statusCmd)

	if statusOut == "active" {
		if userPart == "" {
			if !archiveSilent && utils.PromptConfirm("Disabling archive_mode requires a database restart to take effect. Restart now?") {
				stopOldService(instanceName, meta.User)
				startNewService(instanceName, meta.User)
			} else {
				fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("warn_archive_disable_restart")))
			}
		} else {
			if meta.User == "root" {
				utils.RunCmd("systemctl", "reload", fmt.Sprintf("postgresql-%s.service", instanceName))
			} else {
				utils.RunAsUser(meta.User, fmt.Sprintf("systemctl --user reload postgresql-%s.service", instanceName))
			}
			fmt.Fprintln(os.Stderr, text.FgGreen.Sprint(i18n.T("postgres_reloaded")))
		}

		restartArchiverBackend(instanceName, meta)
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": instanceName, "archive_mode": map[bool]string{true: "off", false: "on"}[userPart == ""], "status": "pg_mgr_archive_disabled"})
	}
	if !UI.Quiet {
		fmt.Println(text.FgGreen.Sprint(i18n.T("archive_disable_success", instanceName)))
	}
	fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("archive_check_notice", meta.Port)))
	return nil
}

func restartArchiverBackend(instanceName string, meta config.InstanceMeta) {
	connection, err := database.Resolve(instanceName, meta, true)
	if err != nil {
		return
	}
	binDir := utils.GetInstanceBinDir(meta)
	psqlBin := "psql"
	if binDir != "" {
		candidate := filepath.Join(binDir, "psql")
		if _, err := os.Stat(candidate); err == nil {
			psqlBin = candidate
		}
	}

	sqlCmd := "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE backend_type = 'archiver';"
	cmd := fmt.Sprintf("%s -p %s -d %s -U %s -c %q", psqlBin, meta.Port, connection.Database, connection.User, sqlCmd)

	out, err := utils.RunAsUserWithOutputForInstance(meta.User, meta, cmd)
	if err == nil && !strings.Contains(out, "FATAL") && !strings.Contains(out, "ERROR") {
		fmt.Println(text.FgGreen.Sprint(i18n.T("archive_process_terminated")))
	}
}
