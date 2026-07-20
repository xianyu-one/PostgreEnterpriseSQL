package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var (
	pgrmanInstance string
	pgrmanMode     string
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: i18n.T("backup_desc"),
}

var pgrmanCmd = &cobra.Command{
	Use:   "pgrman",
	Short: i18n.T("pgrman_desc"),
}

var pgrmanInitCmd = &cobra.Command{
	Use:   "init",
	Short: i18n.T("pgrman_init_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runPgrmanInit() },
}

var pgrmanUninitCmd = &cobra.Command{
	Use:     "uninit",
	Aliases: []string{"clean"},
	Short:   i18n.T("pgrman_uninit_desc"),
	Run:     func(cmd *cobra.Command, args []string) { runPgrmanUninit() },
}

var pgrmanShowCmd = &cobra.Command{
	Use:   "show",
	Short: i18n.T("pgrman_show_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runPgrmanShow() },
}

var backupListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   i18n.T("backup_list_desc"),
	Run:     func(cmd *cobra.Command, args []string) { runBackupList() },
}

var pgrmanRunCmd = &cobra.Command{
	Use:   "run",
	Short: i18n.T("pgrman_run_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runPgrmanRun(cmd) },
}

func init() {
	pgrmanShowCmd.Flags().StringVarP(&pgrmanInstance, "instance", "i", "", "Instance name")
	pgrmanRunCmd.Flags().StringVarP(&pgrmanInstance, "instance", "i", "", "Instance name")
	pgrmanRunCmd.Flags().StringVarP(&pgrmanMode, "mode", "m", "full", "Backup mode (full or incremental)")

	// Autocomplete for instance flag
	compFunc := func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var list []string
		for name := range config.Global.Instances {
			list = append(list, name)
		}
		return list, cobra.ShellCompDirectiveNoFileComp
	}
	pgrmanShowCmd.RegisterFlagCompletionFunc("instance", compFunc)
	pgrmanRunCmd.RegisterFlagCompletionFunc("instance", compFunc)

	pgrmanCmd.AddCommand(pgrmanInitCmd, pgrmanUninitCmd, pgrmanShowCmd, pgrmanRunCmd)
	backupCmd.AddCommand(pgrmanCmd, backupListCmd)
	RootCmd.AddCommand(backupCmd)
}

func getFullCronExpr(bk *config.PgrmanConfig) string {
	if bk == nil {
		return "0 2 * * 0"
	}
	if bk.FullBackupCron != "" {
		return bk.FullBackupCron
	}
	return fmt.Sprintf("%d %d * * %d", bk.FullBackupMin, bk.FullBackupHour, bk.FullBackupDay)
}

func getIncrCronExpr(bk *config.PgrmanConfig) string {
	if bk == nil {
		return "0 3 * * *"
	}
	if bk.IncrBackupCron != "" {
		return bk.IncrBackupCron
	}
	return fmt.Sprintf("%d %d * * *", bk.IncrBackupMin, bk.IncrBackupHour)
}

func promptCron(label string, defaultVal string) string {
	for {
		valStr := utils.PromptInput(label, defaultVal)
		valStr = strings.TrimSpace(valStr)
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		_, err := parser.Parse(valStr)
		if err == nil {
			return valStr
		}
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_cron", err)))
	}
}

func getPgrmanBin(meta config.InstanceMeta) string {
	if meta.BinPath != "" {
		candidate := filepath.Join(meta.BinPath, "pg_rman")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "pg_rman"
}

func runPgrmanInit() {
	ensureRoot()

	if len(config.Global.Instances) == 0 {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_instances")))
		os.Exit(1)
	}

	// List instances and ask user to choose
	fmt.Println("Available instances:")
	var instNames []string
	for name := range config.Global.Instances {
		fmt.Printf(" - %s\n", name)
		instNames = append(instNames, name)
	}

	selectedInst := utils.PromptInput(i18n.T("prompt_backup_inst"), instNames[0])
	meta, ok := config.Global.Instances[selectedInst]
	if !ok {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_inst_not_found", selectedInst)))
		os.Exit(1)
	}

	// Default values
	defaultBackupDir := filepath.Join(config.Global.BaseDir, "backup", selectedInst)
	defaultSrvLog := filepath.Join(meta.DataDir, "log")
	defaultArcLog := filepath.Join(config.Global.BaseDir, "archive", selectedInst)
	defaultCompress := "YES"
	defaultKeepArc := 7
	defaultKeepSrv := 14
	defaultKeepData := 14
	defaultFullCron := "0 2 * * 0"
	defaultIncrCron := "0 3 * * *"

	// Pre-fill from existing pgrman config if available
	if meta.Pgrman != nil {
		bk := meta.Pgrman
		if bk.BackupDir != "" {
			defaultBackupDir = bk.BackupDir
		}
		if bk.SrvLogPath != "" {
			defaultSrvLog = bk.SrvLogPath
		}
		if bk.ArcLogPath != "" {
			defaultArcLog = bk.ArcLogPath
		}
		if bk.CompressData != "" {
			defaultCompress = bk.CompressData
		}
		if bk.KeepArcLogDays > 0 {
			defaultKeepArc = bk.KeepArcLogDays
		}
		if bk.KeepSrvLogDays > 0 {
			defaultKeepSrv = bk.KeepSrvLogDays
		}
		if bk.KeepDataDays > 0 {
			defaultKeepData = bk.KeepDataDays
		}
		defaultFullCron = getFullCronExpr(bk)
		defaultIncrCron = getIncrCronExpr(bk)
	}

	// Interactive Wizard
	backupDir := utils.PromptInput(i18n.T("prompt_backup_dir"), defaultBackupDir)
	srvLogPath := utils.PromptInput(i18n.T("prompt_srv_log"), defaultSrvLog)
	arcLogPath := utils.PromptInput(i18n.T("prompt_arc_log"), defaultArcLog)
	compressData := utils.PromptInput(i18n.T("prompt_compress"), defaultCompress)

	keepArc := promptInt(i18n.T("prompt_keep_arc"), defaultKeepArc)
	keepSrv := promptInt(i18n.T("prompt_keep_srv"), defaultKeepSrv)
	keepData := promptInt(i18n.T("prompt_keep_data"), defaultKeepData)

	fullCron := promptCron(i18n.T("prompt_full_cron"), defaultFullCron)
	incrCron := promptCron(i18n.T("prompt_incr_cron"), defaultIncrCron)

	// Validate user ID and group ID
	u, err := user.Lookup(meta.User)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// Ensure directories exist and chown to postgres/instance user
	err = os.MkdirAll(backupDir, 0755)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}
	_ = os.Chown(backupDir, uid, gid)

	err = os.MkdirAll(arcLogPath, 0755)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}
	_ = os.Chown(arcLogPath, uid, gid)

	// Run pg_rman init with explicit data directory -D
	pgrmanBin := getPgrmanBin(meta)
	initCmdStr := fmt.Sprintf("%s init -B %s -D %s", pgrmanBin, backupDir, meta.DataDir)
	fmt.Printf("Initializing pg_rman in directory: %s...\n", backupDir)
	cmd := exec.Command("su", "-s", "/bin/bash", "-", meta.User, "-c", initCmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(out)
		if strings.Contains(strings.ToLower(outStr), "already initialized") {
			fmt.Printf("pg_rman init notice: %s\n", outStr)
		} else {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_pgrman_init_failed", outStr)))
			os.Exit(1)
		}
	}

	// Write pg_rman.ini config
	iniPath := filepath.Join(backupDir, "pg_rman.ini")
	iniContent := fmt.Sprintf("SRVLOG_PATH='%s'\nARCLOG_PATH='%s'\nCOMPRESS_DATA=%s\nKEEP_ARCLOG_DAYS=%d\nKEEP_SRVLOG_DAYS=%d\nKEEP_DATA_DAYS=%d\n",
		srvLogPath, arcLogPath, compressData, keepArc, keepSrv, keepData)

	err = os.WriteFile(iniPath, []byte(iniContent), 0644)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}
	_ = os.Chown(iniPath, uid, gid)

	// Save to config
	pgrmanConfig := &config.PgrmanConfig{
		Tool:           "pgrman",
		BackupDir:      backupDir,
		SrvLogPath:     srvLogPath,
		ArcLogPath:     arcLogPath,
		CompressData:   compressData,
		KeepArcLogDays: keepArc,
		KeepSrvLogDays: keepSrv,
		KeepDataDays:   keepData,
		FullBackupCron: fullCron,
		IncrBackupCron: incrCron,
	}

	err = config.SaveInstancePgrmanConfig(selectedInst, pgrmanConfig)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}

	fmt.Println(text.FgHiGreen.Sprint(i18n.T("backup_success")))
}

func runPgrmanUninit() {
	ensureRoot()

	var configured []string
	for name, meta := range config.Global.Instances {
		if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" {
			configured = append(configured, name)
		}
	}

	if len(configured) == 0 {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_configured_instances")))
		os.Exit(1)
	}

	fmt.Println("Available instances with backup configuration:")
	for _, name := range configured {
		fmt.Printf(" - %s\n", name)
	}

	selectedInst := utils.PromptInput(i18n.T("prompt_backup_inst"), configured[0])
	meta, ok := config.Global.Instances[selectedInst]
	if !ok || meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_backup_config", selectedInst)))
		os.Exit(1)
	}

	backupDir := meta.Pgrman.BackupDir

	fmt.Println(i18n.T("prompt_uninit_choice_header"))
	fmt.Println(i18n.T("prompt_uninit_opt1"))
	fmt.Println(i18n.T("prompt_uninit_opt2"))
	choice := utils.PromptInput(i18n.T("prompt_choice_12"), "1")

	if choice != "1" && choice != "2" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_choice")))
		os.Exit(1)
	}

	if choice == "2" {
		confirm := utils.PromptInput(i18n.T("confirm_delete_backup_dir", backupDir), "N")
		if strings.ToLower(confirm) == "y" || strings.ToLower(confirm) == "yes" {
			if backupDir != "" && backupDir != "/" {
				err := os.RemoveAll(backupDir)
				if err != nil {
					fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
				} else {
					fmt.Println(i18n.T("backup_dir_deleted", backupDir))
				}
			}
		}
	}

	err := config.SaveInstancePgrmanConfig(selectedInst, nil)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}

	fmt.Println(text.FgHiGreen.Sprint(i18n.T("uninit_success", selectedInst)))
}

func runPgrmanShow() {
	ensureRoot()

	instName := pgrmanInstance
	if instName == "" {
		var configured []string
		for name, meta := range config.Global.Instances {
			if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" {
				configured = append(configured, name)
			}
		}
		if len(configured) == 0 {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_instances")))
			os.Exit(1)
		}
		instName = utils.PromptInput(i18n.T("prompt_backup_inst"), configured[0])
	}

	meta, ok := config.Global.Instances[instName]
	if !ok {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_inst_not_found", instName)))
		os.Exit(1)
	}
	if meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_backup_config", instName)))
		os.Exit(1)
	}

	pgrmanBin := getPgrmanBin(meta)
	showCmdStr := fmt.Sprintf("%s show -B %s -D %s detail", pgrmanBin, meta.Pgrman.BackupDir, meta.DataDir)
	cmd := exec.Command("su", "-s", "/bin/bash", "-", meta.User, "-c", showCmdStr)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
	}
}

func runPgrmanRun(cmd *cobra.Command) {
	ensureRoot()

	instName := pgrmanInstance
	if instName == "" {
		var configured []string
		for name, meta := range config.Global.Instances {
			if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" {
				configured = append(configured, name)
			}
		}
		if len(configured) == 0 {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_instances")))
			os.Exit(1)
		}
		instName = utils.PromptInput(i18n.T("prompt_backup_inst"), configured[0])
	}

	meta, ok := config.Global.Instances[instName]
	if !ok {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_inst_not_found", instName)))
		os.Exit(1)
	}
	if meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_backup_config", instName)))
		os.Exit(1)
	}

	var mode string
	if cmd != nil && cmd.Flags().Changed("mode") {
		m := strings.ToLower(pgrmanMode)
		if m == "full" {
			mode = "full"
		} else if m == "incremental" || m == "incr" {
			mode = "incremental"
		} else {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_mode")))
			os.Exit(1)
		}
	} else {
		fmt.Println(i18n.T("prompt_mode_choice_header"))
		fmt.Println(i18n.T("prompt_mode_opt1"))
		fmt.Println(i18n.T("prompt_mode_opt2"))
		choice := utils.PromptInput(i18n.T("prompt_choice_12"), "1")
		if choice == "1" || strings.ToLower(choice) == "full" {
			mode = "full"
		} else if choice == "2" || strings.ToLower(choice) == "incremental" || strings.ToLower(choice) == "incr" {
			mode = "incremental"
		} else {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_mode")))
			os.Exit(1)
		}
	}

	pgrmanBin := getPgrmanBin(meta)
	runCmdStr := fmt.Sprintf("%s backup -p %s -D %s --backup-mode=%s --with-serverlog -B %s && %s validate -B %s",
		pgrmanBin, meta.Port, meta.DataDir, mode, meta.Pgrman.BackupDir, pgrmanBin, meta.Pgrman.BackupDir)

	fmt.Printf("Running manual backup (mode: %s) for instance '%s' as user '%s'...\n", mode, instName, meta.User)
	execCmd := exec.Command("su", "-s", "/bin/bash", "-", meta.User, "-c", runCmdStr)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	err := execCmd.Run()
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
	} else {
		fmt.Println(text.FgHiGreen.Sprint(i18n.T("done")))
	}
}

func runBackupList() {
	ensureRoot()

	out, _ := exec.Command("systemctl", "is-active", "pg_mgr.service").Output()
	statusStr := strings.TrimSpace(string(out))
	if statusStr == "" {
		statusStr = "inactive"
	}
	daemonStatusText := text.FgHiRed.Sprint(statusStr)
	if statusStr == "active" {
		daemonStatusText = text.FgHiGreen.Sprint(statusStr)
	}

	fmt.Printf("%s: %s\n\n", i18n.T("lbl_daemon_status"), daemonStatusText)

	if len(config.Global.Instances) == 0 {
		fmt.Println(text.FgHiYellow.Sprint(i18n.T("err_no_instances")))
		return
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		i18n.T("tbl_inst"),
		i18n.T("tbl_backup_tool"),
		i18n.T("tbl_backup_dir"),
		i18n.T("tbl_full_cron"),
		i18n.T("tbl_incr_cron"),
		i18n.T("tbl_status"),
	})

	for name, meta := range config.Global.Instances {
		if meta.Pgrman != nil && meta.Pgrman.Tool != "" {
			toolName := meta.Pgrman.Tool
			if toolName == "pgrman" {
				toolName = "pg_rman"
			}
			t.AppendRow(table.Row{
				text.FgHiCyan.Sprint(name),
				toolName,
				meta.Pgrman.BackupDir,
				getFullCronExpr(meta.Pgrman),
				getIncrCronExpr(meta.Pgrman),
				text.FgHiGreen.Sprint(i18n.T("status_configured")),
			})
		} else {
			t.AppendRow(table.Row{
				text.FgHiCyan.Sprint(name),
				"N/A",
				"N/A",
				"N/A",
				"N/A",
				text.FgHiRed.Sprint(i18n.T("status_unconfigured")),
			})
		}
	}

	t.AppendSeparator()
	t.SetStyle(table.StyleLight)
	t.Render()
}

func promptInt(label string, defaultVal int) int {
	valStr := utils.PromptInput(label, strconv.Itoa(defaultVal))
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}
