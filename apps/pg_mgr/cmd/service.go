package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var startCmd = &cobra.Command{
	Use:   "start [instance_name]",
	Short: i18n.T("start_desc"),
	Args:  cobra.MaximumNArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runServiceCmd("start", args) },
}

var stopCmd = &cobra.Command{
	Use:   "stop [instance_name]",
	Short: i18n.T("stop_desc"),
	Args:  cobra.MaximumNArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runServiceCmd("stop", args) },
}

var restartCmd = &cobra.Command{
	Use:   "restart [instance_name]",
	Short: i18n.T("restart_desc"),
	Args:  cobra.MaximumNArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runServiceCmd("restart", args) },
}

var reloadCmd = &cobra.Command{
	Use:   "reload [instance_name]",
	Short: i18n.T("reload_desc"),
	Args:  cobra.MaximumNArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runServiceCmd("reload", args) },
}

var statusCmd = &cobra.Command{
	Use:   "status [instance_name]",
	Short: i18n.T("status_desc"),
	Args:  cobra.MaximumNArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runServiceCmd("status", args) },
}

var enableCmd = &cobra.Command{
	Use:   "enable [instance_name]",
	Short: i18n.T("enable_desc"),
	Args:  cobra.MaximumNArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runServiceCmd("enable", args) },
}

var disableCmd = &cobra.Command{
	Use:   "disable [instance_name]",
	Short: i18n.T("disable_desc"),
	Args:  cobra.MaximumNArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runServiceCmd("disable", args) },
}

func init() {
	compFunc := func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var list []string
		for name := range config.Global.Instances {
			list = append(list, name)
		}
		return list, cobra.ShellCompDirectiveNoFileComp
	}

	startCmd.ValidArgsFunction = compFunc
	stopCmd.ValidArgsFunction = compFunc
	restartCmd.ValidArgsFunction = compFunc
	reloadCmd.ValidArgsFunction = compFunc
	statusCmd.ValidArgsFunction = compFunc
	enableCmd.ValidArgsFunction = compFunc
	disableCmd.ValidArgsFunction = compFunc

	RootCmd.AddCommand(startCmd, stopCmd, restartCmd, reloadCmd, statusCmd, enableCmd, disableCmd)
	InstanceCmd.AddCommand(startCmd, stopCmd, restartCmd, reloadCmd, statusCmd, enableCmd, disableCmd)
}

func runServiceCmd(action string, args []string) {
	instanceName := "default"
	if len(args) > 0 {
		instanceName = args[0]
	}
	manageService(action, instanceName)
}

func manageService(action string, instanceName string) {
	utils.EnsureInstancePermission(instanceName)

	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		fmt.Println(i18n.T("err_not_reg", instanceName))
		os.Exit(1)
	}

	if action == "status" {
		if meta.User == "root" {
			cmd := exec.Command("systemctl", "status", fmt.Sprintf("postgresql-%s.service", instanceName))
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			_ = cmd.Run()
		} else {
			cmdStr := fmt.Sprintf("systemctl --user status postgresql-%s.service", instanceName)
			fullCmdStr := utils.BuildInstanceCmd(meta, cmdStr)
			currUser, err := utils.GetCurrentOSUser()
			var cmd *exec.Cmd
			if err == nil && currUser == meta.User {
				cmd = exec.Command("bash", "-c", fullCmdStr)
			} else {
				cmd = exec.Command("su", "-s", "/bin/bash", "-", meta.User, "-c", fullCmdStr)
			}
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			_ = cmd.Run()
		}
		return
	}

	var err error
	if meta.User == "root" {
		fmt.Printf("Executing: %s for instance '%s' as user 'root'...\n", action, instanceName)
		err = utils.RunCmd("systemctl", action, fmt.Sprintf("postgresql-%s.service", instanceName))
	} else {
		cmd := fmt.Sprintf("systemctl --user %s postgresql-%s.service", action, instanceName)
		fmt.Printf("Executing: %s for instance '%s' as user '%s'...\n", action, instanceName, meta.User)
		err = utils.RunAsUserForInstance(meta.User, meta, cmd)
	}

	if err != nil {
		fmt.Println(i18n.T("err_failed", err))
	} else {
		fmt.Println(text.FgHiGreen.Sprint(i18n.T("done")))
	}
}
