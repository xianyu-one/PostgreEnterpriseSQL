package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/database"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var (
	pgrmanInstance       string
	pgrmanMode           string
	pgrmanEditBackupDir  string
	pgrmanEditSrvLog     string
	pgrmanEditArcLog     string
	pgrmanEditCompress   string
	pgrmanEditKeepArc    int
	pgrmanEditKeepSrv    int
	pgrmanEditKeepData   int
	pgrmanEditFullCron   string
	pgrmanEditIncrCron   string
	pgrmanEditMigrate    bool
	pgrmanEditSchedule   bool
	runPgrmanInitForEdit = func(meta config.InstanceMeta, command string) ([]byte, error) {
		out, err := utils.RunAsUserWithCombinedOutputForInstance(meta.User, meta, command)
		return []byte(out), err
	}
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

var pgrmanEditCmd = &cobra.Command{
	Use:     "modify",
	Aliases: []string{"edit", "set"},
	Short:   i18n.T("pgrman_edit_desc"),
	Run:     func(cmd *cobra.Command, args []string) { runPgrmanEdit(cmd) },
}

var pgrmanUninitCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"uninit", "clean"},
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

var pgrmanDeleteCmd = &cobra.Command{
	Use:   "delete DATE",
	Short: i18n.T("pgrman_delete_desc"),
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runPgrmanDelete(args[0]) },
}

func init() {
	pgrmanShowCmd.Flags().StringVarP(&pgrmanInstance, "instance", "i", "", "Instance name")
	pgrmanRunCmd.Flags().StringVarP(&pgrmanInstance, "instance", "i", "", "Instance name")
	pgrmanDeleteCmd.Flags().StringVarP(&pgrmanInstance, "instance", "i", "", "Instance name")
	pgrmanRunCmd.Flags().StringVarP(&pgrmanMode, "mode", "m", "full", "Backup mode (full or incremental)")

	pgrmanEditCmd.Flags().StringVarP(&pgrmanInstance, "instance", "i", "", "Instance name")
	pgrmanEditCmd.Flags().StringVarP(&pgrmanEditBackupDir, "backup-dir", "B", "", "Backup directory (-B)")
	pgrmanEditCmd.Flags().StringVar(&pgrmanEditSrvLog, "srv-log", "", "Server log directory (SRVLOG_PATH)")
	pgrmanEditCmd.Flags().StringVar(&pgrmanEditArcLog, "arc-log", "", "Archive log directory (ARCLOG_PATH)")
	pgrmanEditCmd.Flags().StringVar(&pgrmanEditCompress, "compress", "", "Compress backup data (YES/NO)")
	pgrmanEditCmd.Flags().IntVar(&pgrmanEditKeepArc, "keep-arc-days", 0, "Retention days for archive logs (KEEP_ARCLOG_DAYS)")
	pgrmanEditCmd.Flags().IntVar(&pgrmanEditKeepSrv, "keep-srv-days", 0, "Retention days for server logs (KEEP_SRVLOG_DAYS)")
	pgrmanEditCmd.Flags().IntVar(&pgrmanEditKeepData, "keep-data-days", 0, "Retention days for backup data (KEEP_DATA_DAYS)")
	pgrmanEditCmd.Flags().StringVar(&pgrmanEditFullCron, "full-cron", "", "Full backup Crontab schedule")
	pgrmanEditCmd.Flags().StringVar(&pgrmanEditIncrCron, "incr-cron", "", "Incremental backup Crontab schedule")
	pgrmanEditCmd.Flags().BoolVar(&pgrmanEditSchedule, "schedule", true, "Enable or disable scheduled backups")
	pgrmanEditCmd.Flags().BoolVarP(&pgrmanEditMigrate, "migrate", "m", false, "Migrate existing backup files to the new directory")

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
	pgrmanDeleteCmd.RegisterFlagCompletionFunc("instance", compFunc)
	pgrmanEditCmd.RegisterFlagCompletionFunc("instance", compFunc)

	pgrmanCmd.AddCommand(pgrmanInitCmd, pgrmanUninitCmd, pgrmanEditCmd, pgrmanShowCmd, pgrmanRunCmd, pgrmanDeleteCmd)
	backupCmd.AddCommand(pgrmanCmd, backupListCmd, pgrmanInitCmd, pgrmanUninitCmd, pgrmanEditCmd, pgrmanShowCmd, pgrmanRunCmd)
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

func isBackupScheduleEnabled(bk *config.PgrmanConfig) bool {
	return bk == nil || bk.ScheduleEnabled == nil || *bk.ScheduleEnabled
}

func boolPtr(value bool) *bool {
	return &value
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
	return utils.GetPgrmanBin(meta)
}

func ensureInstancePermission(instName string) {
	if utils.IsRoot() {
		return
	}
	if meta, ok := config.Global.Instances[instName]; ok {
		if utils.IsRootOrUser(meta.User) {
			return
		}
	}
	ensureRoot()
}

func runPgrmanInit() {
	if len(config.Global.Instances) == 0 {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_instances")))
		os.Exit(1)
	}

	selectedInst, err := promptInstance(i18n.T("prompt_select_instance"), nil)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(err))
		return
	}
	meta, ok := config.Global.Instances[selectedInst]
	if !ok {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_inst_not_found", selectedInst)))
		os.Exit(1)
	}
	ensureInstancePermission(selectedInst)

	// Default values
	defaultBackupDir := filepath.Join(config.Global.BaseDir, "backup", selectedInst)
	defaultSrvLog := filepath.Join(meta.DataDir, "log")
	defaultArcLog := filepath.Join(config.Global.BaseDir, "archive", selectedInst)

	confPath := filepath.Join(meta.DataDir, "postgresql.conf")
	if pgmgrArcDir := utils.GetPgMgrArchiveDir(confPath); pgmgrArcDir != "" {
		defaultArcLog = pgmgrArcDir
	}

	defaultCompress := "YES"
	defaultKeepArc := 7
	defaultKeepSrv := 14
	defaultKeepData := 14
	defaultFullCron := "0 2 * * 0"
	defaultIncrCron := "0 3 * * *"
	scheduleEnabled := true

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
		scheduleEnabled = isBackupScheduleEnabled(bk)
	}

	// Interactive Wizard
	backupDir := utils.PromptPath(i18n.T("prompt_backup_dir"), defaultBackupDir)
	srvLogPath := utils.PromptPath(i18n.T("prompt_srv_log"), defaultSrvLog)
	arcLogPath := utils.PromptPath(i18n.T("prompt_arc_log"), defaultArcLog)
	compressData := utils.PromptInput(i18n.T("prompt_compress"), defaultCompress)

	keepArc := promptInt(i18n.T("prompt_keep_arc"), defaultKeepArc)
	keepSrv := promptInt(i18n.T("prompt_keep_srv"), defaultKeepSrv)
	keepData := promptInt(i18n.T("prompt_keep_data"), defaultKeepData)

	scheduleEnabled = utils.PromptBool(i18n.T("prompt_backup_schedule"), scheduleEnabled)
	fullCron := defaultFullCron
	incrCron := defaultIncrCron
	if scheduleEnabled {
		fullCron = promptCron(i18n.T("prompt_full_cron"), defaultFullCron)
		incrCron = promptCron(i18n.T("prompt_incr_cron"), defaultIncrCron)
	}

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
	initCmdStr := fmt.Sprintf("%s init -B %s -D %s",
		shellQuote(pgrmanBin),
		shellQuote(backupDir),
		shellQuote(meta.DataDir),
	)
	fmt.Printf("Initializing pg_rman in directory: %s...\n", backupDir)
	outStr, err := utils.RunAsUserWithCombinedOutputForInstance(meta.User, meta, initCmdStr)
	if err != nil {
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
		Tool:            "pgrman",
		BackupDir:       backupDir,
		SrvLogPath:      srvLogPath,
		ArcLogPath:      arcLogPath,
		CompressData:    compressData,
		KeepArcLogDays:  keepArc,
		KeepSrvLogDays:  keepSrv,
		KeepDataDays:    keepData,
		FullBackupCron:  fullCron,
		IncrBackupCron:  incrCron,
		ScheduleEnabled: boolPtr(scheduleEnabled),
	}

	err = config.SaveInstancePgrmanConfig(selectedInst, pgrmanConfig)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}

	fmt.Println(text.FgHiGreen.Sprint(i18n.T("backup_success")))
}

func runPgrmanUninit() {
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

	selectedInst, err := promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(err))
		return
	}
	meta, ok := config.Global.Instances[selectedInst]
	if !ok || meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_backup_config", selectedInst)))
		os.Exit(1)
	}
	ensureInstancePermission(selectedInst)

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

	err = config.SaveInstancePgrmanConfig(selectedInst, nil)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}

	fmt.Println(text.FgHiGreen.Sprint(i18n.T("uninit_success", selectedInst)))
}

func runPgrmanShow() {
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
		selected, err := promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(err))
			return
		}
		instName = selected
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
	ensureInstancePermission(instName)

	pgrmanBin := getPgrmanBin(meta)
	showCmdStr := fmt.Sprintf("%s show -B %s -D %s detail", pgrmanBin, meta.Pgrman.BackupDir, meta.DataDir)
	execCmdStr := utils.BuildInstanceCmd(meta, showCmdStr)
	currUser, _ := utils.GetCurrentOSUser()
	var cmd *exec.Cmd
	if currUser == meta.User {
		cmd = exec.Command("bash", "-c", execCmdStr)
	} else {
		cmd = exec.Command("su", "-s", "/bin/bash", "-", meta.User, "-c", execCmdStr)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
	}
}

func runPgrmanRun(cmd *cobra.Command) {
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
		selected, err := promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(err))
			return
		}
		instName = selected
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
	ensureInstancePermission(instName)
	connection, err := database.Resolve(instName, meta, true)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(err))
		return
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

	runCmdStr := buildPgRmanBackupCommand(meta, mode, connection)

	fmt.Printf("Running manual backup (mode: %s) for instance '%s' as user '%s'...\n", mode, instName, meta.User)
	execCmdStr := utils.BuildInstanceCmd(meta, runCmdStr)
	currUser, _ := utils.GetCurrentOSUser()
	var execCmd *exec.Cmd
	if currUser == meta.User {
		execCmd = exec.Command("bash", "-c", execCmdStr)
	} else {
		execCmd = exec.Command("su", "-s", "/bin/bash", "-", meta.User, "-c", execCmdStr)
	}
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	err = execCmd.Run()
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
	} else {
		fmt.Println(text.FgHiGreen.Sprint(i18n.T("done")))
	}
}

func buildPgRmanBackupCommand(meta config.InstanceMeta, mode string, connection database.Connection) string {
	pgrmanBin := getPgrmanBin(meta)
	return fmt.Sprintf("%s backup -p %s -U %s -d %s -D %s --backup-mode=%s --with-serverlog -B %s && %s validate -B %s",
		shellQuote(pgrmanBin),
		shellQuote(meta.Port),
		shellQuote(connection.User),
		shellQuote(connection.Database),
		shellQuote(meta.DataDir),
		shellQuote(mode),
		shellQuote(meta.Pgrman.BackupDir),
		shellQuote(pgrmanBin),
		shellQuote(meta.Pgrman.BackupDir))
}

func validatePgrmanBackupDate(date string) error {
	if _, err := time.Parse("2006-01-02 15:04:05", date); err != nil {
		return errors.New(i18n.T("err_invalid_backup_date"))
	}
	return nil
}

func buildPgrmanDeleteCommand(meta config.InstanceMeta, date string) string {
	return fmt.Sprintf("%s delete %s -B %s -D %s",
		shellQuote(getPgrmanBin(meta)),
		shellQuote(date),
		shellQuote(meta.Pgrman.BackupDir),
		shellQuote(meta.DataDir))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func runPgrmanDelete(date string) {
	if err := validatePgrmanBackupDate(date); err != nil {
		fmt.Println(text.FgHiRed.Sprint(err))
		return
	}

	instName := pgrmanInstance
	if instName == "" {
		var configured []string
		for name, meta := range config.Global.Instances {
			if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" {
				configured = append(configured, name)
			}
		}
		if len(configured) == 0 {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_configured_instances")))
			return
		}
		selected, err := promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(err))
			return
		}
		instName = selected
	}

	meta, ok := config.Global.Instances[instName]
	if !ok {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_inst_not_found", instName)))
		return
	}
	if meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_backup_config", instName)))
		return
	}
	ensureInstancePermission(instName)

	deleteCmdStr := buildPgrmanDeleteCommand(meta, date)
	execCmdStr := utils.BuildInstanceCmd(meta, deleteCmdStr)
	currUser, _ := utils.GetCurrentOSUser()
	var execCmd *exec.Cmd
	if currUser == meta.User {
		execCmd = exec.Command("bash", "-c", execCmdStr)
	} else {
		execCmd = exec.Command("su", "-s", "/bin/bash", "-", meta.User, "-c", execCmdStr)
	}
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	fmt.Printf("%s\n", i18n.T("pgrman_delete_start", date, instName))
	if err := execCmd.Run(); err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		return
	}
	fmt.Println(text.FgHiGreen.Sprint(i18n.T("pgrman_delete_success", date)))
}

func runBackupList() {
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
			fullCron := getFullCronExpr(meta.Pgrman)
			incrCron := getIncrCronExpr(meta.Pgrman)
			status := text.FgHiGreen.Sprint(i18n.T("status_configured"))
			if !isBackupScheduleEnabled(meta.Pgrman) {
				fullCron = i18n.T("status_schedule_disabled")
				incrCron = i18n.T("status_schedule_disabled")
				status = text.FgHiYellow.Sprint(i18n.T("status_manual_only"))
			}
			t.AppendRow(table.Row{
				text.FgHiCyan.Sprint(name),
				toolName,
				meta.Pgrman.BackupDir,
				fullCron,
				incrCron,
				status,
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

func runPgrmanEdit(cmd *cobra.Command) {
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

	selectedInst := pgrmanInstance
	if selectedInst == "" {
		selected, err := promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(err))
			return
		}
		selectedInst = selected
	}

	meta, ok := config.Global.Instances[selectedInst]
	if !ok || meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_backup_config", selectedInst)))
		os.Exit(1)
	}

	bk := meta.Pgrman

	hasFlags := cmd.Flags().Changed("backup-dir") ||
		cmd.Flags().Changed("srv-log") ||
		cmd.Flags().Changed("arc-log") ||
		cmd.Flags().Changed("compress") ||
		cmd.Flags().Changed("keep-arc-days") ||
		cmd.Flags().Changed("keep-srv-days") ||
		cmd.Flags().Changed("keep-data-days") ||
		cmd.Flags().Changed("full-cron") ||
		cmd.Flags().Changed("incr-cron") ||
		cmd.Flags().Changed("schedule")

	backupDir := bk.BackupDir
	srvLogPath := bk.SrvLogPath
	arcLogPath := bk.ArcLogPath
	compressData := bk.CompressData
	keepArc := bk.KeepArcLogDays
	keepSrv := bk.KeepSrvLogDays
	keepData := bk.KeepDataDays
	fullCron := getFullCronExpr(bk)
	incrCron := getIncrCronExpr(bk)
	scheduleEnabled := isBackupScheduleEnabled(bk)

	if hasFlags {
		if cmd.Flags().Changed("backup-dir") {
			backupDir = pgrmanEditBackupDir
		}
		if cmd.Flags().Changed("srv-log") {
			srvLogPath = pgrmanEditSrvLog
		}
		if cmd.Flags().Changed("arc-log") {
			arcLogPath = pgrmanEditArcLog
		}
		if cmd.Flags().Changed("compress") {
			compressData = strings.ToUpper(pgrmanEditCompress)
		}
		if cmd.Flags().Changed("keep-arc-days") {
			keepArc = pgrmanEditKeepArc
		}
		if cmd.Flags().Changed("keep-srv-days") {
			keepSrv = pgrmanEditKeepSrv
		}
		if cmd.Flags().Changed("keep-data-days") {
			keepData = pgrmanEditKeepData
		}
		if cmd.Flags().Changed("full-cron") {
			fullCron = pgrmanEditFullCron
			parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
			if _, err := parser.Parse(fullCron); err != nil {
				fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_cron", err)))
				os.Exit(1)
			}
		}
		if cmd.Flags().Changed("incr-cron") {
			incrCron = pgrmanEditIncrCron
			parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
			if _, err := parser.Parse(incrCron); err != nil {
				fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_cron", err)))
				os.Exit(1)
			}
		}
		if cmd.Flags().Changed("schedule") {
			scheduleEnabled = pgrmanEditSchedule
		}
	} else {
		backupDir = utils.PromptPath(i18n.T("prompt_backup_dir"), backupDir)
		srvLogPath = utils.PromptPath(i18n.T("prompt_srv_log"), srvLogPath)
		arcLogPath = utils.PromptPath(i18n.T("prompt_arc_log"), arcLogPath)
		compressData = utils.PromptInput(i18n.T("prompt_compress"), compressData)

		keepArc = promptInt(i18n.T("prompt_keep_arc"), keepArc)
		keepSrv = promptInt(i18n.T("prompt_keep_srv"), keepSrv)
		keepData = promptInt(i18n.T("prompt_keep_data"), keepData)

		scheduleEnabled = utils.PromptBool(i18n.T("prompt_backup_schedule"), scheduleEnabled)
		if scheduleEnabled {
			fullCron = promptCron(i18n.T("prompt_full_cron"), fullCron)
			incrCron = promptCron(i18n.T("prompt_incr_cron"), incrCron)
		}
	}

	newBackupDir := filepath.Clean(backupDir)
	oldBackupDirClean := filepath.Clean(bk.BackupDir)

	if oldBackupDirClean != "" && newBackupDir != oldBackupDirClean {
		doMigrate := pgrmanEditMigrate
		if !doMigrate && !hasFlags {
			if _, err := os.Stat(oldBackupDirClean); err == nil {
				doMigrate = utils.PromptConfirm(i18n.T("prompt_migrate_backup", oldBackupDirClean, newBackupDir))
			}
		}

		if doMigrate {
			fmt.Printf("Migrating backup directory from %s to %s...\n", oldBackupDirClean, newBackupDir)
			if err := utils.MigrateDirectory(oldBackupDirClean, newBackupDir); err != nil {
				fmt.Println(text.FgHiRed.Sprint(i18n.T("err_migrate_backup_failed", err)))
				os.Exit(1)
			} else {
				fmt.Println(text.FgGreen.Sprint(i18n.T("migrate_backup_success", oldBackupDirClean, newBackupDir)))
			}
		}
	}
	backupDir = newBackupDir

	u, err := user.Lookup(meta.User)

	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	needsCatalogInit, err := backupCatalogNeedsInit(oldBackupDirClean, backupDir)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}
	err = os.MkdirAll(backupDir, 0755)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}
	if arcLogPath != "" {
		err = os.MkdirAll(arcLogPath, 0755)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
			os.Exit(1)
		}
		_ = os.Chown(arcLogPath, uid, gid)
	}

	if needsCatalogInit {
		pgrmanBin := getPgrmanBin(meta)
		initCmdStr := fmt.Sprintf("%s init -B %s -D %s", pgrmanBin, backupDir, meta.DataDir)
		out, initErr := runPgrmanInitForEdit(meta, initCmdStr)
		if initErr != nil {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_pgrman_init_failed", strings.TrimSpace(string(out)))))
			return
		}
	}

	iniPath := filepath.Join(backupDir, "pg_rman.ini")
	iniContent := fmt.Sprintf("SRVLOG_PATH='%s'\nARCLOG_PATH='%s'\nCOMPRESS_DATA=%s\nKEEP_ARCLOG_DAYS=%d\nKEEP_SRVLOG_DAYS=%d\nKEEP_DATA_DAYS=%d\n",
		srvLogPath, arcLogPath, compressData, keepArc, keepSrv, keepData)

	err = os.WriteFile(iniPath, []byte(iniContent), 0644)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}
	_ = os.Chown(iniPath, uid, gid)

	updatedConfig := &config.PgrmanConfig{
		Tool:            "pgrman",
		BackupDir:       backupDir,
		SrvLogPath:      srvLogPath,
		ArcLogPath:      arcLogPath,
		CompressData:    compressData,
		KeepArcLogDays:  keepArc,
		KeepSrvLogDays:  keepSrv,
		KeepDataDays:    keepData,
		FullBackupCron:  fullCron,
		IncrBackupCron:  incrCron,
		ScheduleEnabled: boolPtr(scheduleEnabled),
	}

	err = config.SaveInstancePgrmanConfig(selectedInst, updatedConfig)
	if err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}

	fmt.Println(text.FgHiGreen.Sprint(i18n.T("pgrman_edit_success", selectedInst)))
}

func backupCatalogNeedsInit(oldBackupDir, newBackupDir string) (bool, error) {
	if oldBackupDir == newBackupDir {
		return false, nil
	}
	entries, err := os.ReadDir(newBackupDir)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
