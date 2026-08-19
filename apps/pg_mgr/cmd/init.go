package cmd

import (
	"fmt"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: i18n.T("init_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runInit() },
}

func init() {
	RootCmd.AddCommand(initCmd)
}

func runInit() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}

	baseDir := utils.PromptPath(i18n.T("prompt_global_base"), config.Global.BaseDir)
	logDir := utils.PromptPath(i18n.T("prompt_global_log_dir"), config.Global.LogDir)
	logLevel := utils.PromptInput(i18n.T("prompt_global_log_level"), config.Global.LogLevel)

	logLevelLower := strings.ToLower(strings.TrimSpace(logLevel))
	if logLevelLower != "debug" && logLevelLower != "info" && logLevelLower != "warn" && logLevelLower != "error" {
		logLevel = "error"
	} else {
		logLevel = logLevelLower
	}

	if err := config.SaveGlobalConfig(baseDir, logDir, logLevel); err != nil {
		return err
	}

	fmt.Println(text.FgHiGreen.Sprint(i18n.T("init_success")))
	return nil
}
