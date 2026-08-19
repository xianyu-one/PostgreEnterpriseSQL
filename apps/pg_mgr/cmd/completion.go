package cmd

import (
	"fmt"
	"os"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
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
	RunE:      func(cmd *cobra.Command, args []string) error { return handleCompletion(cmd, args, "install") },
}

var compUninstallCmd = &cobra.Command{
	Use:       "remove [bash|zsh]",
	Aliases:   []string{"uninstall"},
	Short:     "Uninstall completion script",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh"},
	RunE:      func(cmd *cobra.Command, args []string) error { return handleCompletion(cmd, args, "uninstall") },
}

func init() {
	completionCmd.AddCommand(compInstallCmd, compUninstallCmd)
	RootCmd.AddCommand(completionCmd)
}

func handleCompletion(cmd *cobra.Command, args []string, action string) error {
	if len(args) == 0 {
		cmd.Help()
		return nil
	}
	shell := args[0]

	if err := utils.CheckRoot(); err != nil {
		return err
	}

	var targetFile string
	switch shell {
	case "bash":
		targetFile = "/etc/bash_completion.d/pg_mgr"
	case "zsh":
		targetFile = "/usr/local/share/zsh/site-functions/_pg_mgr"
	default:
		fmt.Fprintln(os.Stderr, i18n.T("completion_unsupported"))
		return fmt.Errorf("unsupported shell %q", shell)
	}

	if action == "install" {
		if shell == "zsh" {
			os.MkdirAll("/usr/local/share/zsh/site-functions", 0755)
		}
		file, err := os.OpenFile(targetFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
			return err
		}
		defer file.Close()

		if shell == "bash" {
			cmd.Root().GenBashCompletion(file)
		} else {
			cmd.Root().GenZshCompletion(file)
		}
		fmt.Printf("%s\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")), i18n.T("completion_reload"))
	} else if action == "uninstall" {
		if err := os.Remove(targetFile); err != nil && !os.IsNotExist(err) {
			return err
		} else {
			fmt.Println(text.FgHiGreen.Sprint(i18n.T("done")))
		}
	}
	return nil
}
