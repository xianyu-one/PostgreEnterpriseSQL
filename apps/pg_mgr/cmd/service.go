package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var startCmd = &cobra.Command{
	Use:   "start [instance_name]",
	Short: i18n.T("start_desc"),
	Args:  cobra.MaximumNArgs(1),
	RunE:  serviceHandler("start"),
}

var stopCmd = &cobra.Command{
	Use:   "stop [instance_name]",
	Short: i18n.T("stop_desc"),
	Args:  cobra.MaximumNArgs(1),
	RunE:  serviceHandler("stop"),
}

var restartCmd = &cobra.Command{
	Use:   "restart [instance_name]",
	Short: i18n.T("restart_desc"),
	Args:  cobra.MaximumNArgs(1),
	RunE:  serviceHandler("restart"),
}

var reloadCmd = &cobra.Command{
	Use:   "reload [instance_name]",
	Short: i18n.T("reload_desc"),
	Args:  cobra.MaximumNArgs(1),
	RunE:  serviceHandler("reload"),
}

var statusCmd = &cobra.Command{
	Use:   "status [instance_name]",
	Short: i18n.T("status_desc"),
	Args:  cobra.MaximumNArgs(1),
	RunE:  serviceHandler("status"),
}

var enableCmd = &cobra.Command{
	Use:   "enable [instance_name]",
	Short: i18n.T("enable_desc"),
	Args:  cobra.MaximumNArgs(1),
	RunE:  serviceHandler("enable"),
}

var disableCmd = &cobra.Command{
	Use:   "disable [instance_name]",
	Short: i18n.T("disable_desc"),
	Args:  cobra.MaximumNArgs(1),
	RunE:  serviceHandler("disable"),
}

func serviceHandler(action string) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		if len(args) == 0 && UI.NonInteractive {
			return interaction.MissingFlags("instance_name")
		}
		return runServiceCmd(action, args)
	}
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

func runServiceCmd(action string, args []string) error {
	instanceName := ""
	if len(args) > 0 {
		instanceName = args[0]
	} else {
		selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
		if err != nil {
			return err
		}
		instanceName = selected
	}
	return manageService(action, instanceName)
}

func manageService(action string, instanceName string) error {
	if err := utils.CheckInstancePermission(instanceName); err != nil {
		return err
	}

	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_not_reg", instanceName), interaction.ExitTarget).WithDetail("instance", instanceName)
	}

	if action == "status" {
		var statusCommand *exec.Cmd
		if meta.User == "root" {
			statusCommand = exec.Command("systemctl", "status", fmt.Sprintf("postgresql-%s.service", instanceName))
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
			statusCommand = cmd
		}
		if UI.Output == string(interaction.OutputJSON) {
			output, err := statusCommand.CombinedOutput()
			if err != nil {
				return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_failed", err), interaction.ExitExecution).WithCause(err).WithDetail("output", string(output))
			}
			return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": instanceName, "status": string(output)})
		}
		statusCommand.Stdout = os.Stdout
		statusCommand.Stderr = os.Stderr
		if err := statusCommand.Run(); err != nil {
			return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_failed", err), interaction.ExitExecution).WithCause(err)
		}
		return nil
	}

	var err error
	if meta.User == "root" {
		fmt.Fprintln(os.Stderr, i18n.T("service_executing", action, instanceName, "root"))
		err = utils.RunCmd("systemctl", action, fmt.Sprintf("postgresql-%s.service", instanceName))
	} else {
		cmd := fmt.Sprintf("systemctl --user %s postgresql-%s.service", action, instanceName)
		fmt.Fprintln(os.Stderr, i18n.T("service_executing", action, instanceName, meta.User))
		err = utils.RunAsUserForInstance(meta.User, meta, cmd)
	}

	if err != nil {
		return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_failed", err), interaction.ExitExecution).WithCause(err)
	}
	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": instanceName, "action": action, "status": "completed"})
	}
	if !UI.Quiet {
		fmt.Println(text.FgHiGreen.Sprint(i18n.T("done")))
	}
	return nil
}
