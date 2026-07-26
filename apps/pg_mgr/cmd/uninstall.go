package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var uninstallCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"remove-instance", "uninstall"},
	Short:   i18n.T("uninstall_desc"),
	Run:     func(cmd *cobra.Command, args []string) { runUninstall() },
}

func init() {
	uninstallCmd.Flags().StringVarP(&Config.InstanceName, "instance", "i", "default", "Instance name to uninstall")
	uninstallCmd.RegisterFlagCompletionFunc("instance", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var list []string
		for name := range config.Global.Instances {
			list = append(list, name)
		}
		return list, cobra.ShellCompDirectiveNoFileComp
	})
	uninstallCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, "Run in silent mode without prompts")

	InstanceCmd.AddCommand(uninstallCmd)
	RootCmd.AddCommand(uninstallCmd)
}

func runUninstall() {
	if !Config.Silent {
		selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(err))
			return
		}
		Config.InstanceName = selected
		if !utils.PromptConfirm(i18n.T("confirm_uninst", Config.InstanceName)) {
			fmt.Println(i18n.T("abort"))
			return
		}
	}
	utils.EnsureInstancePermission(Config.InstanceName)

	meta, ok := config.Global.Instances[Config.InstanceName]
	if !ok {
		fmt.Println(i18n.T("err_not_reg", Config.InstanceName))
		os.Exit(1)
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

	pw := progress.NewWriter()
	pw.SetAutoStop(false)
	pw.SetTrackerLength(25)
	pw.SetMessageWidth(40)
	pw.Style().Colors = progress.StyleColorsExample
	pw.Style().Options.DoneString = "✓"
	pw.Style().Options.ErrorString = "✗"
	go pw.Render()

	executeStep := func(msg string, action func() error) {
		tracker := progress.Tracker{Message: msg, Total: 1, Units: progress.UnitsDefault}
		pw.AppendTracker(&tracker)
		if err := action(); err != nil {
			tracker.MarkAsErrored()
			pw.Stop()
			fmt.Printf("\n%s\n", text.FgHiRed.Sprint(i18n.T("err_failed", err)))
			os.Exit(1)
		}
		tracker.MarkAsDone()
	}

	serviceName := fmt.Sprintf("postgresql-%s.service", Config.InstanceName)

	executeStep(i18n.T("stop_service"), func() error {
		if osUser == "root" {
			return utils.RunCmd("systemctl", "stop", serviceName)
		}
		return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user stop %s", serviceName))
	})

	executeStep(i18n.T("disable_service"), func() error {
		if osUser == "root" {
			return utils.RunCmd("systemctl", "disable", serviceName)
		}
		return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user disable %s", serviceName))
	})

	executeStep(i18n.T("remove_service"), func() error {
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
	})

	// Commit registry removal before deleting data. If the shared configuration
	// is not writable, abort instead of reporting success with a stale instance.
	executeStep(i18n.T("remove_config"), func() error {
		return config.RemoveInstanceFromRegistry(Config.InstanceName)
	})

	executeStep(i18n.T("remove_data"), func() error {
		if err := os.RemoveAll(meta.DataDir); err != nil {
			return err
		}
		if deleteBackupDir && backupDir != "" && backupDir != "." && backupDir != "/" {
			if err := os.RemoveAll(backupDir); err != nil {
				return err
			}
		}
		return nil
	})

	pw.Stop()
	fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
}
