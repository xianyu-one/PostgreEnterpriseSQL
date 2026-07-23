package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
)

type InstallConfig struct {
	TarPath      string
	InstanceName string
	MajorVersion string
	MinorVersion string
	Port         int
	Password     string
	Silent       bool
	DataDir      string
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
