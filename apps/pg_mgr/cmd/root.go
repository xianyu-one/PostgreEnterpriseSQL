package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

type InstallConfig struct {
	TarPath        string
	InstanceName   string
	MajorVersion   string
	MinorVersion   string
	Port           int
	Password       string
	PasswordEnv    string
	PasswordFile   string
	Silent         bool
	DataDir        string
	OSUser         string
	DBUser         string
	SystemctlAlias bool
}

var Config InstallConfig

var Version = "dev"

type InteractionOptions struct {
	NonInteractive bool
	LegacySilent   bool
	Yes            bool
	Output         string
	Language       string
	Color          string
	Quiet          bool
	Verbose        bool
}

var UI InteractionOptions

var RootCmd = &cobra.Command{
	Use:     "pg_mgr",
	Short:   "PostgreSQL Enterprise Manager",
	Long:    `pg_mgr is a pure Go tool that brings enterprise-level management capabilities to PostgreSQL databases.`,
	Version: Version,
}

var InstanceCmd = &cobra.Command{
	Use:     "instance",
	Aliases: []string{"inst"},
	Short:   "Manage PostgreSQL database instances",
}

var PkgCmd = &cobra.Command{
	Use:     "pkg",
	Aliases: []string{"package"},
	Short:   "Manage PostgreSQL software packages",
}

func Execute() int {
	if runtime.GOOS != "linux" {
		return renderRootError(os.Stdout, os.Stderr, interaction.NewError(
			interaction.CodeExecutionFailed,
			"This tool is designed specifically for Linux OS.",
			interaction.ExitExecution,
		))
	}

	if err := RootCmd.Execute(); err != nil {
		return renderRootError(os.Stdout, os.Stderr, err)
	}
	return 0
}

func renderRootError(stdout, stderr io.Writer, err error) int {
	message := err.Error()
	if strings.HasPrefix(message, "unknown command") || strings.Contains(message, "unknown flag") || strings.Contains(message, "requires exactly") || strings.Contains(message, "accepts ") {
		err = interaction.NewError(interaction.CodeInvalidInput, message, interaction.ExitUsage).WithCause(err)
	}
	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	if UI.Color == string(interaction.ColorAlways) {
		text.EnableColors()
	} else if UI.Color == string(interaction.ColorNever) || os.Getenv("NO_COLOR") != "" || !term.IsTerminal(int(os.Stderr.Fd())) {
		text.DisableColors()
	}
	renderer := interaction.NewRenderer(stdout, stderr, mode, UI.Quiet)
	if renderErr := renderer.Error(err); renderErr != nil {
		return int(interaction.ExitExecution)
	}
	return interaction.ExitCode(err)
}

func init() {
	cobra.OnInitialize(config.InitConfig)
	i18n.InitLang()

	RootCmd.AddCommand(InstanceCmd)
	RootCmd.AddCommand(PkgCmd)

	flags := RootCmd.PersistentFlags()
	flags.BoolVar(&UI.NonInteractive, "non-interactive", false, i18n.T("flag_non_interactive"))
	flags.BoolVar(&UI.LegacySilent, "silent", false, i18n.T("flag_silent_deprecated"))
	_ = flags.MarkDeprecated("silent", i18n.T("flag_silent_replacement"))
	flags.BoolVar(&UI.Yes, "yes", false, i18n.T("flag_yes"))
	flags.StringVar(&UI.Output, "output", string(interaction.OutputTable), i18n.T("flag_output"))
	flags.StringVar(&UI.Language, "lang", "", i18n.T("flag_language"))
	flags.StringVar(&UI.Color, "color", string(interaction.ColorAuto), i18n.T("flag_color"))
	flags.BoolVar(&UI.Quiet, "quiet", false, i18n.T("flag_quiet"))
	flags.BoolVar(&UI.Verbose, "verbose", false, i18n.T("flag_verbose"))

	RootCmd.SilenceErrors = true
	RootCmd.SilenceUsage = true
	RootCmd.PersistentPreRunE = validateInteractionOptions
}

func validateInteractionOptions(cmd *cobra.Command, _ []string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
		UI.NonInteractive = true
	}
	UI.Output = strings.ToLower(UI.Output)
	if UI.Output != string(interaction.OutputTable) && UI.Output != string(interaction.OutputJSON) {
		return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_output_mode"), interaction.ExitUsage)
	}
	UI.Color = strings.ToLower(UI.Color)
	if UI.Color != string(interaction.ColorAuto) && UI.Color != string(interaction.ColorAlways) && UI.Color != string(interaction.ColorNever) {
		return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_color_mode"), interaction.ExitUsage)
	}
	if UI.Language != "" {
		if UI.Language != "en" && UI.Language != "zh-CN" {
			return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_language"), interaction.ExitUsage)
		}
		i18n.SetLang(UI.Language)
	}
	if UI.Output == string(interaction.OutputJSON) {
		UI.NonInteractive = true
		UI.Color = string(interaction.ColorNever)
	}
	if UI.LegacySilent {
		UI.NonInteractive = true
	}
	// During the compatibility release, legacy leaf flags still feed their
	// existing command fields. The canonical flag applies the same no-prompt
	// policy without granting confirmation.
	if UI.NonInteractive {
		Config.Silent = true
		archiveSilent = true
		UpgConfig.Silent = true
	}
	if flag := cmd.Flags().Lookup("silent"); flag != nil && flag.Changed {
		UI.NonInteractive = true
		UI.LegacySilent = true
	}
	return nil
}

func requireExplicitIdentity(nonInteractive, legacySilent bool, instance *string, password string) error {
	if !nonInteractive {
		return nil
	}
	missing := make([]string, 0, 2)
	if *instance == "" {
		if legacySilent {
			*instance = "default"
		} else {
			missing = append(missing, "--instance")
		}
	}
	if password == "" {
		missing = append(missing, "--password (or a protected secret source)")
	}
	if len(missing) > 0 {
		return interaction.MissingFlags(missing...)
	}
	return nil
}

func promptInstalledVersion(label string, versions []utils.PGVersion, defaultIndex int) (utils.PGVersion, error) {
	if len(versions) == 0 {
		return utils.PGVersion{}, fmt.Errorf("no installed PostgreSQL versions")
	}
	t := table.NewWriter()
	t.SetOutputMirror(os.Stderr)
	t.AppendHeader(table.Row{"#", i18n.T("tbl_ver_version"), i18n.T("tbl_ver_path")})
	for i, version := range versions {
		t.AppendRow(table.Row{i + 1, version.Raw, filepath.Join(config.Global.BaseDir, strconv.Itoa(version.Major), strconv.Itoa(version.Minor))})
	}
	t.Render()
	index, err := utils.PromptSelect(label, len(versions), defaultIndex)
	if err != nil {
		return utils.PGVersion{}, err
	}
	return versions[index], nil
}

func promptInstance(label string, filter func(string, config.InstanceMeta) bool) (string, error) {
	names := make([]string, 0, len(config.Global.Instances))
	for name, meta := range config.Global.Instances {
		if filter == nil || filter(name, meta) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", fmt.Errorf("no available instances")
	}
	t := table.NewWriter()
	t.SetOutputMirror(os.Stderr)
	t.AppendHeader(table.Row{"#", i18n.T("tbl_inst"), i18n.T("tbl_user"), i18n.T("tbl_db_user"), i18n.T("tbl_database_name"), i18n.T("tbl_port"), i18n.T("tbl_datadir"), i18n.T("tbl_ver_path")})
	for i, name := range names {
		meta := config.Global.Instances[name]
		databaseUser := meta.DatabaseUser
		if databaseUser == "" {
			databaseUser = i18n.T("status_pending_detection")
		}
		databaseName := meta.DatabaseName
		if databaseName == "" {
			databaseName = i18n.T("status_pending_detection")
		}
		t.AppendRow(table.Row{i + 1, name, meta.User, databaseUser, databaseName, meta.Port, meta.DataDir, filepath.Dir(filepath.Dir(meta.BinPath))})
	}
	t.Render()
	index, err := utils.PromptSelect(label, len(names), 0)
	if err != nil {
		return "", err
	}
	return names[index], nil
}

func hasPgrmanConfig(_ string, meta config.InstanceMeta) bool {
	return meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman"
}
