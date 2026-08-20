package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var (
	modifyPort            string
	modifyBinPath         string
	modifyDataDir         string
	modifyOSUser          string
	modifyDBUser          string
	modifyDatabaseName    string
	modifyMigrate         bool
	modifyCheckRoot       = func() bool { return utils.IsRoot() }
	modifyCheckPermission = func(instanceName string) bool {
		if modifyCheckRoot() {
			return true
		}
		meta, ok := config.Global.Instances[instanceName]
		if !ok {
			return false
		}
		return utils.IsRootOrUser(meta.User)
	}
	modifyWriteSystemdService = writeSystemdService
	modifyStartNewService     = startNewServiceChecked
)

var modifyCmd = &cobra.Command{
	Use:     "modify [instance_name]",
	Aliases: []string{"configure", "edit"},
	Short:   i18n.T("modify_desc"),
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		instanceName := ""
		if len(args) == 1 {
			instanceName = args[0]
		}
		if UI.NonInteractive && len(args) == 0 {
			return interaction.MissingFlags("instance_name")
		}
		if instanceName == "" {
			selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
			if err != nil {
				return err
			}
			instanceName = selected
		}
		if err := utils.CheckInstancePermission(instanceName); err != nil {
			return err
		}
		if UI.NonInteractive && !hasModifyChanges() {
			return interaction.MissingFlags("one of --port, --bin-path, --data-dir, --os-user, --db-user, --database")
		}
		if !UI.NonInteractive && !hasModifyChanges() {
			if err := promptModifyField(); err != nil {
				return err
			}
		}
		if !UI.NonInteractive {
			if err := reviewModify(instanceName); err != nil {
				return err
			}
		}
		return runModify(instanceName)
	},
}

func reviewModify(instanceName string) error {
	for {
		meta, ok := config.Global.Instances[instanceName]
		if !ok {
			return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_not_reg", instanceName), interaction.ExitTarget)
		}
		value := func(candidate, current string) string {
			if candidate != "" {
				return candidate
			}
			return current
		}
		interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputTable, UI.Quiet).Review(i18n.T("review_modify_instance"), []interaction.ReviewField{
			{Label: i18n.T("tbl_inst"), Value: instanceName},
			{Label: i18n.T("tbl_port"), Value: value(modifyPort, meta.Port)},
			{Label: i18n.T("tbl_ver_path"), Value: value(modifyBinPath, meta.BinPath)},
			{Label: i18n.T("tbl_datadir"), Value: value(modifyDataDir, meta.DataDir)},
			{Label: i18n.T("tbl_user"), Value: value(modifyOSUser, meta.User)},
			{Label: i18n.T("tbl_db_user"), Value: value(modifyDBUser, meta.DatabaseUser)},
			{Label: i18n.T("tbl_database_name"), Value: value(modifyDatabaseName, meta.DatabaseName)},
		})
		choice, err := interaction.NewPrompt(os.Stdin, os.Stderr).Menu(i18n.T("review_modify_instance"), []string{i18n.T("review_execute"), i18n.T("review_modify")}, 0)
		if err != nil {
			return err
		}
		if choice == 0 {
			return nil
		}
		if err := promptModifyField(); err != nil {
			return err
		}
		if modifyOSUser != "" {
			if err := utils.CheckInstancePermission(instanceName); err != nil {
				return err
			}
		}
	}
}

func hasModifyChanges() bool {
	return modifyPort != "" || modifyBinPath != "" || modifyDataDir != "" || modifyOSUser != "" || modifyDBUser != "" || modifyDatabaseName != ""
}

func promptModifyField() error {
	menu := interaction.NewPrompt(os.Stdin, os.Stderr)
	choice, err := menu.Menu(i18n.T("prompt_modify_field"), []string{
		i18n.T("tbl_port"),
		i18n.T("tbl_ver_path"),
		i18n.T("tbl_datadir"),
		i18n.T("tbl_user"),
		i18n.T("tbl_db_user"),
		i18n.T("tbl_database_name"),
	}, 0)
	if err != nil {
		return err
	}
	switch choice {
	case 0:
		modifyPort = utils.PromptInput(i18n.T("prompt_port"), "")
	case 1:
		modifyBinPath = utils.PromptPath(i18n.T("prompt_bin_path"), "")
	case 2:
		modifyDataDir = utils.PromptPath(i18n.T("prompt_data_dir"), "")
	case 3:
		modifyOSUser = utils.PromptInput(i18n.T("prompt_os_user"), "")
	case 4:
		modifyDBUser = utils.PromptInput(i18n.T("prompt_db_user"), "")
	case 5:
		modifyDatabaseName = utils.PromptInput(i18n.T("prompt_database_name"), "")
	}
	if !hasModifyChanges() {
		return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_modify_no_flags"), interaction.ExitUsage)
	}
	return nil
}

func init() {
	modifyCmd.Flags().StringVarP(&modifyPort, "port", "p", "", "New port for the database instance")
	modifyCmd.Flags().StringVarP(&modifyBinPath, "bin-path", "b", "", "New path to the postgres binary")
	modifyCmd.Flags().StringVarP(&modifyDataDir, "data-dir", "d", "", "New data directory for the instance")
	modifyCmd.Flags().StringVarP(&modifyOSUser, "os-user", "u", "", "New OS user who runs the database instance")
	modifyCmd.Flags().StringVar(&modifyDBUser, "db-user", "", "Database superuser used for instance connections")
	modifyCmd.Flags().StringVar(&modifyDatabaseName, "database", "", "Default database used for instance connections")
	modifyCmd.Flags().BoolVarP(&modifyMigrate, "migrate", "m", false, "Migrate existing data directory to the new location")

	compFunc := func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var list []string
		for name := range config.Global.Instances {
			list = append(list, name)
		}
		return list, cobra.ShellCompDirectiveNoFileComp
	}
	modifyCmd.ValidArgsFunction = compFunc

	RootCmd.AddCommand(modifyCmd)
	InstanceCmd.AddCommand(modifyCmd)
}

func runModify(instanceName string) error {
	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_not_reg", instanceName), interaction.ExitTarget).WithDetail("instance", instanceName)
	}

	if modifyPort == "" && modifyBinPath == "" && modifyDataDir == "" && modifyOSUser == "" && modifyDBUser == "" && modifyDatabaseName == "" {
		return interaction.NewError(interaction.CodeMissingInput, i18n.T("err_modify_no_flags"), interaction.ExitUsage)
	}

	newPort := meta.Port
	if modifyPort != "" {
		newPort = modifyPort
	}
	newBinPath := meta.BinPath
	if modifyBinPath != "" {
		newBinPath = modifyBinPath
	}
	oldDataDir := meta.DataDir
	newDataDir := meta.DataDir
	if modifyDataDir != "" {
		newDataDir = filepath.Clean(modifyDataDir)
	}
	newOSUser := meta.User
	if modifyOSUser != "" {
		newOSUser = modifyOSUser
	}
	newDBUser := meta.DatabaseUser
	if modifyDBUser != "" {
		newDBUser = modifyDBUser
	}
	newDatabaseName := meta.DatabaseName
	if modifyDatabaseName != "" {
		newDatabaseName = modifyDatabaseName
	}

	u, err := user.Lookup(newOSUser)
	if err != nil {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_user_not_found", newOSUser), interaction.ExitTarget).WithCause(err)
	}

	// Check if running
	var statusCmd string
	if meta.User == "root" {
		statusCmd = fmt.Sprintf("systemctl is-active postgresql-%s.service", instanceName)
	} else {
		statusCmd = fmt.Sprintf("systemctl --user is-active postgresql-%s.service", instanceName)
	}
	statusOut, _ := utils.RunAsUserWithOutput(meta.User, statusCmd)
	isActive := (statusOut == "active")

	var restartNeeded bool
	if isActive {
		if UI.NonInteractive && !UI.Yes {
			return interaction.MissingFlags("--yes")
		}
		if UI.Yes || utils.PromptConfirm(i18n.T("prompt_restart_now")) {
			restartNeeded = true
			stopOldService(instanceName, meta.User)
		} else {
			fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("warn_restart_required")))
		}
	}

	dataDirMigrated := false
	// Data Directory Migration if requested
	if modifyDataDir != "" && newDataDir != oldDataDir && modifyMigrate {
		if isActive && !restartNeeded {
			stopOldService(instanceName, meta.User)
		}
		fmt.Fprintln(os.Stderr, i18n.T("migrate_data_start", oldDataDir, newDataDir))
		if err := utils.MigrateDirectory(oldDataDir, newDataDir); err != nil {
			return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_migrate_data_failed", err), interaction.ExitExecution).WithCause(err)
		}
		fmt.Fprintln(os.Stderr, text.FgGreen.Sprint(i18n.T("migrate_data_success", oldDataDir, newDataDir)))
		dataDirMigrated = true

		if meta.Pgrman != nil && meta.Pgrman.SrvLogPath != "" {
			if rel, err := filepath.Rel(oldDataDir, meta.Pgrman.SrvLogPath); err == nil && !strings.HasPrefix(rel, "..") {
				meta.Pgrman.SrvLogPath = filepath.Join(newDataDir, rel)
			}
		}
	}

	// Update postgresql.conf if port changed
	if modifyPort != "" {
		confPath := filepath.Join(newDataDir, "postgresql.conf")
		if err := utils.UpdatePostgresqlConfParam(confPath, "port", newPort); err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("warn_port_update", confPath, err))
		}
		// Also update .pgrc
		pgrcPath := filepath.Join(u.HomeDir, ".pgrc")
		envs := map[string]string{
			"PGPORT": fmt.Sprintf("'%s'", newPort),
		}
		_ = utils.UpdatePgrc(pgrcPath, envs)
	}

	serviceChanged := (modifyBinPath != "" || modifyDataDir != "" || modifyOSUser != "")
	if serviceChanged {
		if err := utils.ChangeInstanceOwnership(instanceName, meta, newDataDir, newOSUser); err != nil {
			fmt.Fprintln(os.Stderr, i18n.T("warn_ownership_update", err))
		}
		if newOSUser != "root" {
			_ = utils.RunCmd("loginctl", "enable-linger", newOSUser)
		}
		deleteSystemdService(instanceName, meta.User)
		if err := modifyWriteSystemdService(instanceName, newOSUser, newBinPath, newDataDir); err != nil {
			return err
		}
	}

	// A migrated cluster must successfully start from the new directory before
	// the registry is committed. This also restarts an active instance even when
	// the earlier generic restart prompt was declined: migration already required
	// stopping it to move the files.
	shouldStart := restartNeeded || (dataDirMigrated && isActive)
	if dataDirMigrated || shouldStart {
		if err := modifyStartNewService(instanceName, newOSUser); err != nil {
			if dataDirMigrated {
				fmt.Fprintln(os.Stderr, text.FgHiRed.Sprint(i18n.T("err_migrate_start_failed", newDataDir, err)))
			} else {
				fmt.Fprintln(os.Stderr, text.FgHiRed.Sprint(i18n.T("err_start_service_failed", err)))
			}
			return err
		}
		if dataDirMigrated && !isActive {
			// Preserve the original stopped state after the startup test.
			stopOldService(instanceName, newOSUser)
		}
	}

	// Save only after the new service has passed its startup test.
	if err := config.SaveInstanceToRegistryWithDatabaseConnection(instanceName, newOSUser, newDataDir, newBinPath, newPort, newDBUser, newDatabaseName); err != nil {
		return err
	}

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	return interaction.NewRenderer(os.Stdout, os.Stderr, mode, UI.Quiet).Success(map[string]any{
		"instance":  instanceName,
		"before":    map[string]any{"port": meta.Port, "bin_path": meta.BinPath, "data_dir": meta.DataDir, "os_user": meta.User, "db_user": meta.DatabaseUser, "database": meta.DatabaseName},
		"after":     map[string]any{"port": newPort, "bin_path": newBinPath, "data_dir": newDataDir, "os_user": newOSUser, "db_user": newDBUser, "database": newDatabaseName},
		"restarted": shouldStart, "status": "modified",
	})
}

func stopOldService(name, osUser string) {
	if osUser == "root" {
		utils.RunCmd("systemctl", "stop", fmt.Sprintf("postgresql-%s.service", name))
	} else {
		utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user stop postgresql-%s.service", name))
	}
}

func startNewService(name, osUser string) {
	_ = startNewServiceChecked(name, osUser)
}

func startNewServiceChecked(name, osUser string) error {
	serviceName := fmt.Sprintf("postgresql-%s.service", name)
	if osUser == "root" {
		if err := utils.RunCmd("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := utils.RunCmd("systemctl", "enable", serviceName); err != nil {
			return err
		}
		return utils.RunCmd("systemctl", "start", serviceName)
	}
	if err := utils.RunAsUser(osUser, "systemctl --user daemon-reload"); err != nil {
		return err
	}
	if err := utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user enable %s", serviceName)); err != nil {
		return err
	}
	return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user start %s", serviceName))
}

func deleteSystemdService(name, osUser string) {
	serviceName := fmt.Sprintf("postgresql-%s.service", name)
	if osUser == "root" {
		utils.RunCmd("systemctl", "disable", serviceName)
		os.Remove(filepath.Join("/etc/systemd/system", serviceName))
		utils.RunCmd("systemctl", "daemon-reload")
	} else {
		u, err := user.Lookup(osUser)
		if err == nil {
			utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user disable %s", serviceName))
			sysdDir := filepath.Join(u.HomeDir, ".config", "systemd", "user")
			os.Remove(filepath.Join(sysdDir, serviceName))
			utils.RunAsUser(osUser, "systemctl --user daemon-reload")
		}
	}
}

func writeSystemdService(name, osUser, binPath, dataDir string) error {
	u, err := user.Lookup(osUser)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	serviceName := fmt.Sprintf("postgresql-%s.service", name)

	var svcPath string
	var wantedBy string
	if osUser == "root" {
		svcPath = filepath.Join("/etc/systemd/system", serviceName)
		wantedBy = "multi-user.target"
	} else {
		utils.RunAsUser(osUser, "mkdir -p ~/.config/systemd/user")
		sysdDir := filepath.Join(u.HomeDir, ".config", "systemd", "user")
		svcPath = filepath.Join(sysdDir, serviceName)
		wantedBy = "default.target"
	}

	svcContent := fmt.Sprintf(`[Unit]
Description=PostgreSQL database server (%s)
Documentation=man:postgres(1)
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=%s -D %s
ExecReload=/bin/kill -HUP $MAINPID
KillMode=mixed
KillSignal=SIGINT
TimeoutSec=infinity
Restart=on-failure

[Install]
WantedBy=%s
`, name, binPath, dataDir, wantedBy)

	if err := os.WriteFile(svcPath, []byte(svcContent), 0644); err != nil {
		return err
	}
	if err := os.Chown(svcPath, uid, gid); err != nil {
		return err
	}
	if osUser == "root" {
		return utils.RunCmd("systemctl", "daemon-reload")
	}
	return utils.RunAsUser(osUser, "systemctl --user daemon-reload")
}
