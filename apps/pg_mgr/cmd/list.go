package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/process"
	"pg_mgr/internal/utils"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   i18n.T("list_desc"),
	RunE:    func(cmd *cobra.Command, args []string) error { return runList() },
}

var listVersionsCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"versions"},
	Short:   i18n.T("list_versions_desc"),
	RunE:    func(cmd *cobra.Command, args []string) error { return runListVersions() },
}

func init() {
	listCmd.AddCommand(listVersionsCmd)
	RootCmd.AddCommand(listCmd)
	InstanceCmd.AddCommand(listCmd)
	PkgCmd.AddCommand(listVersionsCmd)
}

type versionResult struct {
	Version string `json:"version"`
	Path    string `json:"path"`
}

func runListVersions() error {
	baseDir := config.Global.BaseDir
	installed, err := utils.GetInstalledVersions(baseDir)
	if err != nil {
		return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_scan_base", err), interaction.ExitExecution).WithCause(err)
	}
	results := make([]versionResult, 0, len(installed))
	for _, v := range installed {
		results = append(results, versionResult{Version: v.Raw, Path: filepath.Join(baseDir, strconv.Itoa(v.Major), strconv.Itoa(v.Minor))})
	}
	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(results)
	}

	if len(installed) == 0 {
		fmt.Println(text.FgHiYellow.Sprint(i18n.T("no_versions_found")))
		return nil
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
	return nil
}

type instanceResult struct {
	Name      string `json:"name"`
	Managed   bool   `json:"managed"`
	Version   string `json:"version"`
	Port      string `json:"port"`
	OSUser    string `json:"os_user"`
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	AutoStart string `json:"auto_start"`
	DataDir   string `json:"data_dir"`
	CPU       string `json:"cpu"`
	Memory    string `json:"memory"`
}

func runList() error {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		i18n.T("tbl_inst"),
		i18n.T("tbl_ver_version"),
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
	results := make([]instanceResult, 0, len(config.Global.Instances))

	// 1. Scan managed instances from Registry
	names := make([]string, 0, len(config.Global.Instances))
	for name := range config.Global.Instances {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		meta := config.Global.Instances[name]
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
		verStr := utils.GetPGVersion(meta.BinPath, meta.DataDir, meta.User)
		results = append(results, instanceResult{Name: name, Managed: true, Version: verStr, Port: meta.Port, OSUser: meta.User, Status: statusOut, Uptime: uptimeStr, AutoStart: bootOut, DataDir: meta.DataDir, CPU: cpuStr, Memory: memStr})

		t.AppendRow(table.Row{
			text.FgHiCyan.Sprint(name),
			verStr,
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
			verStr := utils.GetPGVersion(proc.BinPath, proc.DataDir, proc.OSUser)
			results = append(results, instanceResult{Name: "", Managed: false, Version: verStr, Port: proc.Port, OSUser: proc.OSUser, Status: "active (pid:" + proc.PID + ")", Uptime: uptimeStr, AutoStart: "N/A", DataDir: proc.DataDir, CPU: cpuStr, Memory: memStr})
			t.AppendRow(table.Row{
				text.FgHiYellow.Sprint("[Unmanaged]"),
				verStr,
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
	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(results)
	}

	if t.Length() == 0 {
		fmt.Println(i18n.T("no_instances_found"))
		return nil
	}

	t.AppendSeparator()
	t.SetStyle(table.StyleLight)
	t.Render()
	return nil
}
