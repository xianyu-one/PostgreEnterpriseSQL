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
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 1 {
			runModify(args[0])
			return
		}
		selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(err))
			return
		}
		runModify(selected)
	},
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

func runModify(instanceName string) {
	if !modifyCheckPermission(instanceName) {
		metaUser := "postgres"
		if meta, ok := config.Global.Instances[instanceName]; ok {
			metaUser = meta.User
		}
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root_or_user", metaUser)))
		os.Exit(1)
	}

	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		fmt.Println(i18n.T("err_not_reg", instanceName))
		os.Exit(1)
	}

	if modifyPort == "" && modifyBinPath == "" && modifyDataDir == "" && modifyOSUser == "" && modifyDBUser == "" && modifyDatabaseName == "" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_modify_no_flags")))
		os.Exit(1)
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
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_user_not_found", newOSUser)))
		os.Exit(1)
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
		if utils.PromptConfirm(i18n.T("prompt_restart_now")) {
			restartNeeded = true
			stopOldService(instanceName, meta.User)
		} else {
			fmt.Println(text.FgHiYellow.Sprint("Warning: Modifications will not take effect until the service is restarted manually."))
		}
	}

	dataDirMigrated := false
	// Data Directory Migration if requested
	if modifyDataDir != "" && newDataDir != oldDataDir && modifyMigrate {
		if isActive && !restartNeeded {
			stopOldService(instanceName, meta.User)
		}
		fmt.Printf("Migrating data directory from %s to %s...\n", oldDataDir, newDataDir)
		if err := utils.MigrateDirectory(oldDataDir, newDataDir); err != nil {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_migrate_data_failed", err)))
			os.Exit(1)
		}
		fmt.Println(text.FgGreen.Sprint(i18n.T("migrate_data_success", oldDataDir, newDataDir)))
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
			fmt.Printf("Warning: Failed to update port in %s: %v\n", confPath, err)
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
			fmt.Printf("Warning: Failed to update directory ownership: %v\n", err)
		}
		if newOSUser != "root" {
			_ = utils.RunCmd("loginctl", "enable-linger", newOSUser)
		}
		deleteSystemdService(instanceName, meta.User)
		if err := modifyWriteSystemdService(instanceName, newOSUser, newBinPath, newDataDir); err != nil {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
			os.Exit(1)
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
				fmt.Println(text.FgHiRed.Sprint(i18n.T("err_migrate_start_failed", newDataDir, err)))
			} else {
				fmt.Println(text.FgHiRed.Sprint(i18n.T("err_start_service_failed", err)))
			}
			os.Exit(1)
		}
		if dataDirMigrated && !isActive {
			// Preserve the original stopped state after the startup test.
			stopOldService(instanceName, newOSUser)
		}
	}

	// Save only after the new service has passed its startup test.
	if err := config.SaveInstanceToRegistryWithDatabaseConnection(instanceName, newOSUser, newDataDir, newBinPath, newPort, newDBUser, newDatabaseName); err != nil {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_failed", err)))
		os.Exit(1)
	}

	if shouldStart {
		fmt.Println(i18n.T("restart_success", instanceName))
	}

	fmt.Println(i18n.T("modify_success", instanceName))
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
