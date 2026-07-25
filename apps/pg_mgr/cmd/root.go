package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

type InstallConfig struct {
	TarPath        string
	InstanceName   string
	MajorVersion   string
	MinorVersion   string
	Port           int
	Password       string
	Silent         bool
	DataDir        string
	OSUser         string
	DBUser         string
	SystemctlAlias bool
}

var Config InstallConfig

var Version = "dev"

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

func Execute() {
	if runtime.GOOS != "linux" {
		fmt.Println(text.FgHiRed.Sprint("This tool is designed specifically for Linux OS."))
		os.Exit(1)
	}

	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(config.InitConfig)
	i18n.InitLang()

	RootCmd.AddCommand(InstanceCmd)
	RootCmd.AddCommand(PkgCmd)
}

func promptInstalledVersion(label string, versions []utils.PGVersion, defaultIndex int) (utils.PGVersion, error) {
	if len(versions) == 0 {
		return utils.PGVersion{}, fmt.Errorf("no installed PostgreSQL versions")
	}
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
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
	t.SetOutputMirror(os.Stdout)
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
