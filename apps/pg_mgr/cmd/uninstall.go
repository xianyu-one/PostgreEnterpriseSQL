package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var uninstallCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"remove-instance", "uninstall"},
	Short:   i18n.T("uninstall_desc"),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareRemoval(cmd); err != nil {
			return err
		}
		return runUninstall()
	},
}

func init() {
	uninstallCmd.Flags().StringVarP(&Config.InstanceName, "instance", "i", "", i18n.T("flag_instance"))
	uninstallCmd.RegisterFlagCompletionFunc("instance", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var list []string
		for name := range config.Global.Instances {
			list = append(list, name)
		}
		return list, cobra.ShellCompDirectiveNoFileComp
	})
	uninstallCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, i18n.T("flag_silent_deprecated"))
	_ = uninstallCmd.Flags().MarkDeprecated("silent", i18n.T("flag_silent_replacement"))

	InstanceCmd.AddCommand(uninstallCmd)
	RootCmd.AddCommand(uninstallCmd)
}

func prepareRemoval(_ *cobra.Command) error {
	if !Config.Silent {
		return nil
	}
	if Config.InstanceName == "" {
		if UI.LegacySilent {
			Config.InstanceName = "default"
			fmt.Fprintln(os.Stderr, i18n.T("warn_legacy_default_instance"))
		} else {
			return interaction.MissingFlags("--instance")
		}
	}
	if !UI.Yes {
		return interaction.MissingFlags("--yes")
	}
	return nil
}

func runUninstall() error {
	if !Config.Silent {
		if Config.InstanceName == "" {
			selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
			if err != nil {
				fmt.Fprintln(os.Stderr, text.FgHiRed.Sprint(err))
				return err
			}
			Config.InstanceName = selected
		}
		if err := utils.CheckInstancePermission(Config.InstanceName); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, i18n.T("removal_instance", Config.InstanceName))
		if meta, ok := config.Global.Instances[Config.InstanceName]; ok {
			fmt.Fprintln(os.Stderr, i18n.T("removal_data_dir", meta.DataDir))
			if meta.Pgrman != nil {
				fmt.Fprintln(os.Stderr, i18n.T("removal_backup_dir", meta.Pgrman.BackupDir))
			}
		}
		if !utils.PromptConfirm(i18n.T("confirm_uninst", Config.InstanceName)) {
			fmt.Fprintln(os.Stderr, i18n.T("abort"))
			return nil
		}
	} else if err := utils.CheckInstancePermission(Config.InstanceName); err != nil {
		return err
	}

	meta, ok := config.Global.Instances[Config.InstanceName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_not_reg", Config.InstanceName), interaction.ExitTarget).
			WithDetail("instance", Config.InstanceName)
	}
	osUser := meta.User
	u, _ := user.Lookup(osUser)
	deleteBackupDir := false
	backupDir := ""
	if meta.Pgrman != nil {
		backupDir = filepath.Clean(meta.Pgrman.BackupDir)
		if backupDir != "" && backupDir != "." && backupDir != "/" && !Config.Silent {
			deleteBackupDir = utils.PromptConfirm(i18n.T("confirm_delete_backup_dir", backupDir))
		}
	}

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	operation := interaction.NewOperation(os.Stderr, mode)
	executeStep := func(msg string, action func() error) error {
		if err := operation.Run(msg, action); err != nil {
			operation.Retain(meta.DataDir)
			operation.RecoverWith(i18n.T("retry_with"))
			return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_failed", err), interaction.ExitExecution).
				WithCause(err).WithDetail("operation", operation.Result())
		}
		return nil
	}

	serviceName := fmt.Sprintf("postgresql-%s.service", Config.InstanceName)

	if err := executeStep(i18n.T("stop_service"), func() error {
		if osUser == "root" {
			return utils.RunCmd("systemctl", "stop", serviceName)
		}
		return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user stop %s", serviceName))
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("disable_service"), func() error {
		if osUser == "root" {
			return utils.RunCmd("systemctl", "disable", serviceName)
		}
		return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user disable %s", serviceName))
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("remove_service"), func() error {
		if osUser == "root" {
			svcPath := filepath.Join("/etc/systemd/system", serviceName)
			os.Remove(svcPath)
			return utils.RunCmd("systemctl", "daemon-reload")
		}
		if u != nil {
			svcPath := filepath.Join(u.HomeDir, ".config", "systemd", "user", serviceName)
			os.Remove(svcPath)
		}
		return utils.RunAsUser(osUser, "systemctl --user daemon-reload")
	}); err != nil {
		return err
	}

	// Commit registry removal before deleting data. If the shared configuration
	// is not writable, abort instead of reporting success with a stale instance.
	if err := executeStep(i18n.T("remove_config"), func() error {
		return config.RemoveInstanceFromRegistry(Config.InstanceName)
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("remove_data"), func() error {
		if err := os.RemoveAll(meta.DataDir); err != nil {
			return err
		}
		if deleteBackupDir && backupDir != "" && backupDir != "." && backupDir != "/" {
			if err := os.RemoveAll(backupDir); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": Config.InstanceName, "status": "removed", "operation": operation.Result()})
	}
	if !UI.Quiet {
		fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
	}
	return nil
}
