package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var (
	modifyPort      string
	modifyBinPath   string
	modifyDataDir   string
	modifyOSUser    string
	modifyCheckRoot = func() bool { return os.Geteuid() == 0 }
)

var modifyCmd = &cobra.Command{
	Use:     "modify [instance_name]",
	Aliases: []string{"configure"},
	Short:   i18n.T("modify_desc"),
	Args:    cobra.ExactArgs(1),
	Run:     func(cmd *cobra.Command, args []string) { runModify(args[0]) },
}

func init() {
	modifyCmd.Flags().StringVarP(&modifyPort, "port", "p", "", "New port for the database instance")
	modifyCmd.Flags().StringVarP(&modifyBinPath, "bin-path", "b", "", "New path to the postgres binary")
	modifyCmd.Flags().StringVarP(&modifyDataDir, "data-dir", "d", "", "New data directory for the instance")
	modifyCmd.Flags().StringVarP(&modifyOSUser, "os-user", "u", "", "New OS user who runs the database instance")

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
}

func runModify(instanceName string) {
	if !modifyCheckRoot() {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root")))
		os.Exit(1)
	}

	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		fmt.Println(i18n.T("err_not_reg", instanceName))
		os.Exit(1)
	}

	if modifyPort == "" && modifyBinPath == "" && modifyDataDir == "" && modifyOSUser == "" {
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
	newDataDir := meta.DataDir
	if modifyDataDir != "" {
		newDataDir = filepath.Clean(modifyDataDir)
	}
	newOSUser := meta.User
	if modifyOSUser != "" {
		newOSUser = modifyOSUser
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

	// Update postgresql.conf if port changed
	if modifyPort != "" {
		confPath := filepath.Join(meta.DataDir, "postgresql.conf")
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
		deleteSystemdService(instanceName, meta.User)
		writeSystemdService(instanceName, newOSUser, newBinPath, newDataDir)
	}

	// Save to registry
	config.SaveInstanceToRegistry(instanceName, newOSUser, newDataDir, newBinPath, newPort)

	if restartNeeded {
		startNewService(instanceName, newOSUser)
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
	serviceName := fmt.Sprintf("postgresql-%s.service", name)
	if osUser == "root" {
		utils.RunCmd("systemctl", "daemon-reload")
		utils.RunCmd("systemctl", "enable", serviceName)
		utils.RunCmd("systemctl", "start", serviceName)
	} else {
		utils.RunAsUser(osUser, "systemctl --user daemon-reload")
		utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user enable %s", serviceName))
		utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user start %s", serviceName))
	}
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

func writeSystemdService(name, osUser, binPath, dataDir string) {
	u, err := user.Lookup(osUser)
	if err != nil {
		return
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

	os.WriteFile(svcPath, []byte(svcContent), 0644)
	os.Chown(svcPath, uid, gid)
}
