package cmd

import (
	"fmt"
	"os"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var startCmd = &cobra.Command{
	Use:   "start [instance_name]",
	Short: i18n.T("start_desc"),
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { manageService("start", args[0]) },
}

var stopCmd = &cobra.Command{
	Use:   "stop [instance_name]",
	Short: i18n.T("stop_desc"),
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { manageService("stop", args[0]) },
}

var enableCmd = &cobra.Command{
	Use:   "enable [instance_name]",
	Short: i18n.T("enable_desc"),
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { manageService("enable", args[0]) },
}

var disableCmd = &cobra.Command{
	Use:   "disable [instance_name]",
	Short: i18n.T("disable_desc"),
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { manageService("disable", args[0]) },
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
	enableCmd.ValidArgsFunction = compFunc
	disableCmd.ValidArgsFunction = compFunc

	RootCmd.AddCommand(startCmd, stopCmd, enableCmd, disableCmd)
}

func manageService(action string, instanceName string) {
	if os.Geteuid() != 0 {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root")))
		os.Exit(1)
	}

	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		fmt.Println(i18n.T("err_not_reg", instanceName))
		os.Exit(1)
	}

	var err error
	if meta.User == "root" {
		fmt.Printf("Executing: %s for instance '%s' as user 'root'...\n", action, instanceName)
		err = utils.RunCmd("systemctl", action, fmt.Sprintf("postgresql-%s.service", instanceName))
	} else {
		cmd := fmt.Sprintf("systemctl --user %s postgresql-%s.service", action, instanceName)
		fmt.Printf("Executing: %s for instance '%s' as user '%s'...\n", action, instanceName, meta.User)
		err = utils.RunAsUser(meta.User, cmd)
	}

	if err != nil {
		fmt.Println(i18n.T("err_failed", err))
	} else {
		fmt.Println(text.FgHiGreen.Sprint(i18n.T("done")))
	}
}
