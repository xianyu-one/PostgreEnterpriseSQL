package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var (
	initBaseDir  string
	initLogDir   string
	initLogLevel string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: i18n.T("init_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runInit() },
}

func init() {
	initCmd.Flags().StringVar(&initBaseDir, "base-dir", "", i18n.T("prompt_global_base"))
	initCmd.Flags().StringVar(&initLogDir, "log-dir", "", i18n.T("prompt_global_log_dir"))
	initCmd.Flags().StringVar(&initLogLevel, "log-level", "", i18n.T("prompt_global_log_level"))
	RootCmd.AddCommand(initCmd)
}

func runInit() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}

	baseDir, logDir, logLevel := initBaseDir, initLogDir, initLogLevel
	if UI.NonInteractive {
		missing := make([]string, 0, 3)
		if baseDir == "" {
			missing = append(missing, "--base-dir")
		}
		if logDir == "" {
			missing = append(missing, "--log-dir")
		}
		if logLevel == "" {
			missing = append(missing, "--log-level")
		}
		if len(missing) > 0 {
			return interaction.MissingFlags(missing...)
		}
	} else {
		if baseDir == "" {
			baseDir = utils.PromptPath(i18n.T("prompt_global_base"), config.Global.BaseDir)
		}
		if logDir == "" {
			logDir = utils.PromptPath(i18n.T("prompt_global_log_dir"), config.Global.LogDir)
		}
		if logLevel == "" {
			logLevel = utils.PromptInput(i18n.T("prompt_global_log_level"), config.Global.LogLevel)
		}
		renderer := interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputTable, UI.Quiet)
		for {
			renderer.Review(i18n.T("review_init"), []interaction.ReviewField{
				{Label: i18n.T("prompt_global_base"), Value: baseDir},
				{Label: i18n.T("prompt_global_log_dir"), Value: logDir},
				{Label: i18n.T("prompt_global_log_level"), Value: logLevel},
			})
			choice, err := interaction.NewPrompt(os.Stdin, os.Stderr).Menu(i18n.T("review_init"), []string{i18n.T("review_execute"), i18n.T("review_modify")}, 0)
			if err != nil {
				return err
			}
			if choice == 0 {
				break
			}
			field, err := interaction.NewPrompt(os.Stdin, os.Stderr).Menu(i18n.T("prompt_modify_field"), []string{i18n.T("prompt_global_base"), i18n.T("prompt_global_log_dir"), i18n.T("prompt_global_log_level")}, 0)
			if err != nil {
				return err
			}
			switch field {
			case 0:
				baseDir = utils.PromptPath(i18n.T("prompt_global_base"), baseDir)
			case 1:
				logDir = utils.PromptPath(i18n.T("prompt_global_log_dir"), logDir)
			case 2:
				logLevel = utils.PromptInput(i18n.T("prompt_global_log_level"), logLevel)
			}
		}
	}

	logLevelLower := strings.ToLower(strings.TrimSpace(logLevel))
	if logLevelLower != "debug" && logLevelLower != "info" && logLevelLower != "warn" && logLevelLower != "error" {
		return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_log_level"), interaction.ExitUsage)
	} else {
		logLevel = logLevelLower
	}

	if err := config.SaveGlobalConfig(baseDir, logDir, logLevel); err != nil {
		return err
	}

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	return interaction.NewRenderer(os.Stdout, os.Stderr, mode, UI.Quiet).Success(map[string]any{
		"base_dir": baseDir, "log_dir": logDir, "log_level": logLevel, "status": "initialized",
	})
}
