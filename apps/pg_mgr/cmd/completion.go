package cmd

import (
	"fmt"
	"os"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/i18n"
)

var completionCmd = &cobra.Command{
	Use:   "completion",
	Short: i18n.T("comp_desc"),
}

var compInstallCmd = &cobra.Command{
	Use:       "install [bash|zsh]",
	Short:     "Install completion script",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh"},
	Run:       func(cmd *cobra.Command, args []string) { handleCompletion(cmd, args, "install") },
}

var compUninstallCmd = &cobra.Command{
	Use:       "uninstall [bash|zsh]",
	Short:     "Uninstall completion script",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh"},
	Run:       func(cmd *cobra.Command, args []string) { handleCompletion(cmd, args, "uninstall") },
}

func init() {
	completionCmd.AddCommand(compInstallCmd, compUninstallCmd)
	RootCmd.AddCommand(completionCmd)
}

func handleCompletion(cmd *cobra.Command, args []string, action string) {
	if len(args) == 0 {
		cmd.Help()
		return
	}
	shell := args[0]

	if os.Geteuid() != 0 {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root")))
		os.Exit(1)
	}

	var targetFile string
	switch shell {
	case "bash":
		targetFile = "/etc/bash_completion.d/pg_mgr"
	case "zsh":
		targetFile = "/usr/local/share/zsh/site-functions/_pg_mgr"
	default:
		fmt.Println("Unsupported shell. Use 'bash' or 'zsh'.")
		return
	}

	if action == "install" {
		if shell == "zsh" {
			os.MkdirAll("/usr/local/share/zsh/site-functions", 0755)
		}
		file, err := os.OpenFile(targetFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
			return
		}
		defer file.Close()

		if shell == "bash" {
			cmd.Root().GenBashCompletion(file)
		} else {
			cmd.Root().GenZshCompletion(file)
		}
		fmt.Printf("%s\nReload your shell to apply changes.\n", text.FgHiGreen.Sprint(i18n.T("done")))
	} else if action == "uninstall" {
		if err := os.Remove(targetFile); err != nil && !os.IsNotExist(err) {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		} else {
			fmt.Println(text.FgHiGreen.Sprint(i18n.T("done")))
		}
	}
}
