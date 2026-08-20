package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
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
		return interaction.NewError(interaction.CodeInvalidInput, i18n.T("completion_unsupported"), interaction.ExitUsage).WithDetail("shell", shell)
	}

	if action == "install" {
		if shell == "zsh" {
			os.MkdirAll("/usr/local/share/zsh/site-functions", 0755)
		}
		file, err := os.OpenFile(targetFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		defer file.Close()

		if shell == "bash" {
			cmd.Root().GenBashCompletion(file)
		} else {
			cmd.Root().GenZshCompletion(file)
		}
	} else if action == "uninstall" {
		if err := os.Remove(targetFile); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	result := map[string]any{"shell": shell, "action": action, "path": targetFile, "status": "completed"}
	if action == "install" {
		result["guidance"] = i18n.T("completion_reload")
	}
	return interaction.NewRenderer(os.Stdout, os.Stderr, mode, UI.Quiet).Success(result)
}
