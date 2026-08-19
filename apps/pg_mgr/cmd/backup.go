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
	"pg_mgr/internal/interaction"
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
	pgrmanDeleteBackups  bool
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
	RunE:  func(cmd *cobra.Command, args []string) error { return runPgrmanInit() },
}

var pgrmanEditCmd = &cobra.Command{
	Use:     "modify",
	Aliases: []string{"edit", "set"},
	Short:   i18n.T("pgrman_edit_desc"),
	RunE:    func(cmd *cobra.Command, args []string) error { return runPgrmanEdit(cmd) },
}

var pgrmanUninitCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"uninit", "clean"},
	Short:   i18n.T("pgrman_uninit_desc"),
	RunE:    func(cmd *cobra.Command, args []string) error { return runPgrmanUninit() },
}

var pgrmanShowCmd = &cobra.Command{
	Use:   "show",
	Short: i18n.T("pgrman_show_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runPgrmanShow() },
}

var backupListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   i18n.T("backup_list_desc"),
	RunE:    func(cmd *cobra.Command, args []string) error { runBackupList(); return nil },
}

var pgrmanRunCmd = &cobra.Command{
	Use:   "run",
	Short: i18n.T("pgrman_run_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runPgrmanRun(cmd) },
}

var pgrmanDeleteCmd = &cobra.Command{
	Use:   "delete DATE",
	Short: i18n.T("pgrman_delete_desc"),
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runPgrmanDelete(args[0]) },
}

func init() {
	pgrmanInitCmd.Flags().StringVarP(&pgrmanInstance, "instance", "i", "", "Instance name")
	pgrmanUninitCmd.Flags().StringVarP(&pgrmanInstance, "instance", "i", "", "Instance name")
	pgrmanUninitCmd.Flags().BoolVar(&pgrmanDeleteBackups, "delete-backups", false, "Delete the backup directory and all backup data")
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

func ensureInstancePermission(instName string) error {
	return utils.CheckInstancePermission(instName)
}

func runPgrmanInit() (runErr error) {
	if len(config.Global.Instances) == 0 {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_instances"), interaction.ExitTarget)
	}

	selectedInst := pgrmanInstance
	if selectedInst == "" {
		if UI.NonInteractive {
			return interaction.MissingFlags("--instance")
		}
		var err error
		selectedInst, err = promptInstance(i18n.T("prompt_select_instance"), nil)
		if err != nil {
			return err
		}
	}
	meta, ok := config.Global.Instances[selectedInst]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_inst_not_found", selectedInst), interaction.ExitTarget)
	}
	if err := ensureInstancePermission(selectedInst); err != nil {
		return err
	}
	if UI.NonInteractive {
		return interaction.MissingFlags("backup configuration flags (non-interactive backup init is not yet configured)")
	}

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
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// Ensure directories exist and chown to postgres/instance user
	err = os.MkdirAll(backupDir, 0755)
	if err != nil {
		return err
	}
	_ = os.Chown(backupDir, uid, gid)

	err = os.MkdirAll(arcLogPath, 0755)
	if err != nil {
		return err
	}
	_ = os.Chown(arcLogPath, uid, gid)

	// Run pg_rman init with explicit data directory -D
	pgrmanBin := getPgrmanBin(meta)
	initCmdStr := fmt.Sprintf("%s init -B %s -D %s",
		shellQuote(pgrmanBin),
		shellQuote(backupDir),
		shellQuote(meta.DataDir),
	)
	fmt.Fprintln(os.Stderr, i18n.T("pgrman_init_start", backupDir))
	outStr, err := utils.RunAsUserWithCombinedOutputForInstance(meta.User, meta, initCmdStr)
	if err != nil {
		if strings.Contains(strings.ToLower(outStr), "already initialized") {
			fmt.Fprintln(os.Stderr, i18n.T("pgrman_init_notice", outStr))
		} else {
			return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_pgrman_init_failed", outStr), interaction.ExitExecution).WithCause(err)
		}
	}

	// Write pg_rman.ini config
	iniPath := filepath.Join(backupDir, "pg_rman.ini")
	iniContent := fmt.Sprintf("SRVLOG_PATH='%s'\nARCLOG_PATH='%s'\nCOMPRESS_DATA=%s\nKEEP_ARCLOG_DAYS=%d\nKEEP_SRVLOG_DAYS=%d\nKEEP_DATA_DAYS=%d\n",
		srvLogPath, arcLogPath, compressData, keepArc, keepSrv, keepData)

	err = os.WriteFile(iniPath, []byte(iniContent), 0644)
	if err != nil {
		return err
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
		return err
	}

	fmt.Println(text.FgHiGreen.Sprint(i18n.T("backup_success")))
	return nil
}

func runPgrmanUninit() error {
	var configured []string
	for name, meta := range config.Global.Instances {
		if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" {
			configured = append(configured, name)
		}
	}

	if len(configured) == 0 {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_configured_instances"), interaction.ExitTarget)
	}

	selectedInst := pgrmanInstance
	if selectedInst == "" {
		if UI.NonInteractive {
			return interaction.MissingFlags("--instance")
		}
		var err error
		selectedInst, err = promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
		if err != nil {
			return err
		}
	}
	meta, ok := config.Global.Instances[selectedInst]
	if !ok || meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_backup_config", selectedInst), interaction.ExitTarget)
	}
	if err := ensureInstancePermission(selectedInst); err != nil {
		return err
	}
	if UI.NonInteractive && !UI.Yes {
		return interaction.MissingFlags("--yes")
	}

	backupDir := meta.Pgrman.BackupDir

	choice := "1"
	if UI.NonInteractive {
		if pgrmanDeleteBackups {
			choice = "2"
		}
	} else {
		fmt.Println(i18n.T("prompt_uninit_choice_header"))
		fmt.Println(i18n.T("prompt_uninit_opt1"))
		fmt.Println(i18n.T("prompt_uninit_opt2"))
		choice = utils.PromptInput(i18n.T("prompt_choice_12"), "1")
	}

	if choice != "1" && choice != "2" {
		return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_invalid_choice"), interaction.ExitUsage)
	}

	if choice == "2" {
		if UI.NonInteractive || utils.PromptConfirm(i18n.T("confirm_delete_backup_dir", backupDir)) {
			if backupDir != "" && backupDir != "/" {
				err := os.RemoveAll(backupDir)
				if err != nil {
					return err
				} else {
					fmt.Fprintln(os.Stderr, i18n.T("backup_dir_deleted", backupDir))
				}
			}
		}
	}

	if err := config.SaveInstancePgrmanConfig(selectedInst, nil); err != nil {
		return err
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": selectedInst, "status": "backup_uninitialized", "backups_deleted": choice == "2"})
	}
	if !UI.Quiet {
		fmt.Println(text.FgHiGreen.Sprint(i18n.T("uninit_success", selectedInst)))
	}
	return nil
}

func runPgrmanShow() error {
	instName := pgrmanInstance
	if instName == "" {
		if UI.NonInteractive {
			return interaction.MissingFlags("--instance")
		}
		var configured []string
		for name, meta := range config.Global.Instances {
			if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" {
				configured = append(configured, name)
			}
		}
		if len(configured) == 0 {
			return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_instances"), interaction.ExitTarget)
		}
		selected, err := promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
		if err != nil {
			return err
		}
		instName = selected
	}

	meta, ok := config.Global.Instances[instName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_inst_not_found", instName), interaction.ExitTarget)
	}
	if meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_backup_config", instName), interaction.ExitTarget)
	}
	if err := ensureInstancePermission(instName); err != nil {
		return err
	}

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
	if UI.Output == string(interaction.OutputJSON) {
		output, err := cmd.CombinedOutput()
		if err != nil {
			return err
		}
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": instName, "catalog": string(output)})
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runPgrmanRun(cmd *cobra.Command) error {
	instName := pgrmanInstance
	if instName == "" {
		if UI.NonInteractive {
			return interaction.MissingFlags("--instance")
		}
		var configured []string
		for name, meta := range config.Global.Instances {
			if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" {
				configured = append(configured, name)
			}
		}
		if len(configured) == 0 {
			return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_instances"), interaction.ExitTarget)
		}
		selected, err := promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
		if err != nil {
			return err
		}
		instName = selected
	}

	meta, ok := config.Global.Instances[instName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_inst_not_found", instName), interaction.ExitTarget)
	}
	if meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_backup_config", instName), interaction.ExitTarget)
	}
	if err := ensureInstancePermission(instName); err != nil {
		return err
	}
	connection, err := database.Resolve(instName, meta, true)
	if err != nil {
		return err
	}

	var mode string
	if cmd != nil && (cmd.Flags().Changed("mode") || UI.NonInteractive) {
		m := strings.ToLower(pgrmanMode)
		if m == "full" {
			mode = "full"
		} else if m == "incremental" || m == "incr" {
			mode = "incremental"
		} else {
			return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_invalid_mode"), interaction.ExitUsage)
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
			return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_invalid_mode"), interaction.ExitUsage)
		}
	}

	runCmdStr := buildPgRmanBackupCommand(meta, mode, connection)

	if UI.Output != string(interaction.OutputJSON) {
		fmt.Fprintln(os.Stderr, i18n.T("pgrman_run_start", mode, instName, meta.User))
	}
	execCmdStr := utils.BuildInstanceCmd(meta, runCmdStr)
	currUser, _ := utils.GetCurrentOSUser()
	var execCmd *exec.Cmd
	if currUser == meta.User {
		execCmd = exec.Command("bash", "-c", execCmdStr)
	} else {
		execCmd = exec.Command("su", "-s", "/bin/bash", "-", meta.User, "-c", execCmdStr)
	}
	if UI.Output == string(interaction.OutputJSON) {
		output, runErr := execCmd.CombinedOutput()
		if runErr != nil {
			return runErr
		}
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": instName, "mode": mode, "status": "completed", "output": string(output)})
	}
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	err = execCmd.Run()
	if err != nil {
		return err
	}
	if !UI.Quiet {
		fmt.Println(text.FgHiGreen.Sprint(i18n.T("done")))
	}
	return nil
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

func runPgrmanDelete(date string) error {
	if err := validatePgrmanBackupDate(date); err != nil {
		return interaction.NewError(interaction.CodeInvalidInput, err.Error(), interaction.ExitUsage).WithCause(err)
	}

	instName := pgrmanInstance
	if instName == "" {
		if UI.NonInteractive {
			return interaction.MissingFlags("--instance")
		}
		var configured []string
		for name, meta := range config.Global.Instances {
			if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" {
				configured = append(configured, name)
			}
		}
		if len(configured) == 0 {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_no_configured_instances")))
			return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_configured_instances"), interaction.ExitTarget)
		}
		selected, err := promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
		if err != nil {
			return err
		}
		instName = selected
	}

	meta, ok := config.Global.Instances[instName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_inst_not_found", instName), interaction.ExitTarget)
	}
	if meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_backup_config", instName), interaction.ExitTarget)
	}
	if err := ensureInstancePermission(instName); err != nil {
		return err
	}
	if UI.NonInteractive && !UI.Yes {
		return interaction.MissingFlags("--yes")
	}

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
	if UI.Output != string(interaction.OutputJSON) {
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
	}

	if UI.Output != string(interaction.OutputJSON) {
		fmt.Printf("%s\n", i18n.T("pgrman_delete_start", date, instName))
	}
	if UI.Output == string(interaction.OutputJSON) {
		output, err := execCmd.CombinedOutput()
		if err != nil {
			return err
		}
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": instName, "deleted_through": date, "status": "deleted", "output": string(output)})
	}
	if err := execCmd.Run(); err != nil {
		return err
	}
	fmt.Println(text.FgHiGreen.Sprint(i18n.T("pgrman_delete_success", date)))
	return nil
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

func runPgrmanEdit(cmd *cobra.Command) (runErr error) {
	if err := ensureRoot(); err != nil {
		return err
	}

	var configured []string
	for name, meta := range config.Global.Instances {
		if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" {
			configured = append(configured, name)
		}
	}

	if len(configured) == 0 {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_configured_instances"), interaction.ExitTarget)
	}

	selectedInst := pgrmanInstance
	if selectedInst == "" {
		if UI.NonInteractive {
			return interaction.MissingFlags("--instance")
		}
		selected, err := promptInstance(i18n.T("prompt_select_instance"), hasPgrmanConfig)
		if err != nil {
			return err
		}
		selectedInst = selected
	}

	meta, ok := config.Global.Instances[selectedInst]
	if !ok || meta.Pgrman == nil || meta.Pgrman.Tool != "pgrman" {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_no_backup_config", selectedInst), interaction.ExitTarget)
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
	if UI.NonInteractive && !hasFlags {
		return interaction.MissingFlags("at least one backup configuration flag")
	}

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
				return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_invalid_cron", err), interaction.ExitUsage).WithCause(err)
			}
		}
		if cmd.Flags().Changed("incr-cron") {
			incrCron = pgrmanEditIncrCron
			parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
			if _, err := parser.Parse(incrCron); err != nil {
				return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_invalid_cron", err), interaction.ExitUsage).WithCause(err)
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
			fmt.Fprintln(os.Stderr, i18n.T("migrate_backup_start", oldBackupDirClean, newBackupDir))
			if err := utils.MigrateDirectory(oldBackupDirClean, newBackupDir); err != nil {
				return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_migrate_backup_failed", err), interaction.ExitExecution).WithCause(err)
			} else {
				fmt.Fprintln(os.Stderr, text.FgGreen.Sprint(i18n.T("migrate_backup_success", oldBackupDirClean, newBackupDir)))
			}
		}
	}
	backupDir = newBackupDir

	u, err := user.Lookup(meta.User)

	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	needsCatalogInit, err := backupCatalogNeedsInit(oldBackupDirClean, backupDir)
	if err != nil {
		return err
	}
	err = os.MkdirAll(backupDir, 0755)
	if err != nil {
		return err
	}
	if arcLogPath != "" {
		err = os.MkdirAll(arcLogPath, 0755)
		if err != nil {
			return err
		}
		_ = os.Chown(arcLogPath, uid, gid)
	}

	if needsCatalogInit {
		pgrmanBin := getPgrmanBin(meta)
		initCmdStr := fmt.Sprintf("%s init -B %s -D %s", pgrmanBin, backupDir, meta.DataDir)
		out, initErr := runPgrmanInitForEdit(meta, initCmdStr)
		if initErr != nil {
			return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_pgrman_init_failed", strings.TrimSpace(string(out))), interaction.ExitExecution).WithCause(initErr)
		}
	}

	iniPath := filepath.Join(backupDir, "pg_rman.ini")
	iniContent := fmt.Sprintf("SRVLOG_PATH='%s'\nARCLOG_PATH='%s'\nCOMPRESS_DATA=%s\nKEEP_ARCLOG_DAYS=%d\nKEEP_SRVLOG_DAYS=%d\nKEEP_DATA_DAYS=%d\n",
		srvLogPath, arcLogPath, compressData, keepArc, keepSrv, keepData)

	err = os.WriteFile(iniPath, []byte(iniContent), 0644)
	if err != nil {
		return err
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
		return err
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": selectedInst, "status": "backup_configuration_updated", "backup_dir": backupDir})
	}
	if !UI.Quiet {
		fmt.Println(text.FgHiGreen.Sprint(i18n.T("pgrman_edit_success", selectedInst)))
	}
	return nil
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
