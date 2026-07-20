package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/process"
	"pg_mgr/internal/utils"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   i18n.T("list_desc"),
	Run:     func(cmd *cobra.Command, args []string) { runList() },
}

var listVersionsCmd = &cobra.Command{
	Use:   "versions",
	Short: i18n.T("list_versions_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runListVersions() },
}

func init() {
	listCmd.AddCommand(listVersionsCmd)
	RootCmd.AddCommand(listCmd)
}

func runListVersions() {
	baseDir := config.Global.BaseDir
	installed, err := utils.GetInstalledVersions(baseDir)
	if err != nil {
		fmt.Printf("Error scanning base directory: %v\n", err)
		os.Exit(1)
	}

	if len(installed) == 0 {
		fmt.Println(text.FgHiYellow.Sprint(i18n.T("no_versions_found")))
		return
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		i18n.T("tbl_ver_version"),
		i18n.T("tbl_ver_path"),
	})

	for _, v := range installed {
		vPath := filepath.Join(baseDir, strconv.Itoa(v.Major), strconv.Itoa(v.Minor))
		t.AppendRow(table.Row{
			text.FgHiCyan.Sprint(v.Raw),
			vPath,
		})
	}

	t.AppendSeparator()
	t.SetStyle(table.StyleLight)
	t.Render()
}

func runList() {
	scanAndSyncInstances()
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		i18n.T("tbl_inst"),
		i18n.T("tbl_port"),
		i18n.T("tbl_user"),
		i18n.T("tbl_status"),
		i18n.T("tbl_uptime"),
		i18n.T("tbl_boot"),
		i18n.T("tbl_datadir"),
		i18n.T("tbl_cpu"),
		i18n.T("tbl_mem"),
	})

	managedDirs := make(map[string]bool)

	// 1. Scan managed instances from Registry
	for name, meta := range config.Global.Instances {
		managedDirs[filepath.Clean(meta.DataDir)] = true

		var statusCmd, bootCmd string
		if meta.User == "root" {
			statusCmd = fmt.Sprintf("systemctl is-active postgresql-%s.service", name)
			bootCmd = fmt.Sprintf("systemctl is-enabled postgresql-%s.service", name)
		} else {
			statusCmd = fmt.Sprintf("systemctl --user is-active postgresql-%s.service", name)
			bootCmd = fmt.Sprintf("systemctl --user is-enabled postgresql-%s.service", name)
		}

		statusOut, _ := utils.RunAsUserWithOutput(meta.User, statusCmd)
		bootOut, _ := utils.RunAsUserWithOutput(meta.User, bootCmd)

		statusText := text.FgHiRed.Sprint(statusOut)
		if statusOut == "active" {
			statusText = text.FgHiGreen.Sprint(statusOut)
		}

		bootText := text.FgHiRed.Sprint(bootOut)
		if bootOut == "enabled" {
			bootText = text.FgHiGreen.Sprint(bootOut)
		}

		cpuStr, memStr := process.GetInstanceResourceUsage(meta.DataDir)
		uptimeStr := process.GetInstanceUptime(meta.DataDir)

		t.AppendRow(table.Row{
			text.FgHiCyan.Sprint(name),
			meta.Port,
			meta.User,
			statusText,
			uptimeStr,
			bootText,
			meta.DataDir,
			cpuStr,
			memStr,
		})
	}

	// 2. Scan orphaned (unmanaged) processes
	runningProcs := process.FindPgProcesses()
	for _, proc := range runningProcs {
		if !managedDirs[filepath.Clean(proc.DataDir)] {
			cpuStr, memStr := process.GetInstanceResourceUsage(proc.DataDir)
			uptimeStr := process.GetInstanceUptime(proc.DataDir, proc.PID)
			t.AppendRow(table.Row{
				text.FgHiYellow.Sprint("[Unmanaged]"),
				proc.Port,
				proc.OSUser,
				text.FgHiGreen.Sprintf("active (pid:%s)", proc.PID),
				uptimeStr,
				text.FgHiRed.Sprint("N/A"),
				proc.DataDir,
				cpuStr,
				memStr,
			})
		}
	}

	if t.Length() == 0 {
		fmt.Println("No PostgreSQL instances found.")
		return
	}

	t.AppendSeparator()
	t.SetStyle(table.StyleLight)
	t.Render()
}
