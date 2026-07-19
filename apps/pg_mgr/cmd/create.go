package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var createInstanceCmd = &cobra.Command{
	Use:     "create-instance",
	Aliases: []string{"create"},
	Short:   i18n.T("create_instance_desc"),
	Run:     func(cmd *cobra.Command, args []string) { runCreateInstance() },
}

func init() {
	createInstanceCmd.Flags().StringVarP(&Config.InstanceName, "instance", "i", "default", "Instance name for multi-instance support")
	createInstanceCmd.Flags().StringVar(&Config.MajorVersion, "major", "16", "Major version path structure")
	createInstanceCmd.Flags().StringVar(&Config.MinorVersion, "minor", "9", "Minor version path structure")
	createInstanceCmd.Flags().StringVar(&Config.DataDir, "data", "", "Data directory path (defaults to base_dir/instances/instance_name)")
	createInstanceCmd.Flags().IntVarP(&Config.Port, "port", "p", 51721, "Database listener port")
	createInstanceCmd.Flags().StringVar(&Config.Password, "password", "SuperSecret123", "Initial password for postgres user")
	createInstanceCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, "Run in silent mode without prompts")

	RootCmd.AddCommand(createInstanceCmd)
}

func runCreateInstance() {
	if os.Geteuid() != 0 {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root")))
		os.Exit(1)
	}

	baseDir := config.Global.BaseDir
	installed, err := utils.GetInstalledVersions(baseDir)
	if err == nil && len(installed) > 0 {
		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{i18n.T("tbl_ver_version"), i18n.T("tbl_ver_path")})
		for _, v := range installed {
			t.AppendRow(table.Row{v.Raw, filepath.Join(baseDir, strconv.Itoa(v.Major), strconv.Itoa(v.Minor))})
		}
		t.Render()

		recommended := installed[len(installed)-1]
		Config.MajorVersion = strconv.Itoa(recommended.Major)
		Config.MinorVersion = strconv.Itoa(recommended.Minor)
	}

	if !Config.Silent {
		Config.InstanceName = utils.PromptInput(i18n.T("prompt_inst"), Config.InstanceName)
		Config.MajorVersion = utils.PromptInput(i18n.T("prompt_major"), Config.MajorVersion)
		Config.MinorVersion = utils.PromptInput(i18n.T("prompt_minor"), Config.MinorVersion)

		defaultDataDir := filepath.Join(baseDir, "instances", Config.InstanceName)
		currentDefault := Config.DataDir
		if currentDefault == "" {
			currentDefault = defaultDataDir
		}
		Config.DataDir = utils.PromptInput(i18n.T("prompt_inst_data_dir"), currentDefault)

		portStr := utils.PromptInput(i18n.T("prompt_port"), strconv.Itoa(Config.Port))
		Config.Port, _ = strconv.Atoi(portStr)
		Config.Password = utils.PromptInput(i18n.T("prompt_pass"), Config.Password)
	} else {
		if Config.DataDir == "" {
			Config.DataDir = filepath.Join(baseDir, "instances", Config.InstanceName)
		}
	}

	versionPathFull := filepath.Join(baseDir, Config.MajorVersion, Config.MinorVersion)
	pgBin := filepath.Join(versionPathFull, "bin", "postgres")

	// Verify software exists at versionPathFull
	if _, err := os.Stat(pgBin); os.IsNotExist(err) {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_version_not_installed", fmt.Sprintf("%s.%s", Config.MajorVersion, Config.MinorVersion))))
		os.Exit(1)
	}

	pw := progress.NewWriter()
	pw.SetAutoStop(false)
	pw.SetTrackerLength(25)
	pw.SetMessageWidth(40)
	pw.Style().Colors = progress.StyleColorsExample
	pw.Style().Options.DoneString = "✓"
	pw.Style().Options.ErrorString = "✗"
	go pw.Render()

	executeStep := func(msg string, action func() error) {
		tracker := progress.Tracker{Message: msg, Total: 1, Units: progress.UnitsDefault}
		pw.AppendTracker(&tracker)
		if err := action(); err != nil {
			tracker.MarkAsErrored()
			pw.Stop()
			fmt.Printf("\n%s\n", text.FgHiRed.Sprint(i18n.T("err_failed", err)))
			os.Exit(1)
		}
		tracker.MarkAsDone()
	}

	executeStep(i18n.T("step_logind"), func() error {
		confPath := "/etc/systemd/logind.conf"
		_ = utils.ReplaceInFile(confPath, `(?m)^#?RemoveIPC=.*`, "RemoveIPC=no")
		content, _ := os.ReadFile(confPath)
		if !strings.Contains(string(content), "RemoveIPC=no") {
			utils.AppendToFile(confPath, "\nRemoveIPC=no\n")
		}
		utils.RunCmd("systemctl", "daemon-reload")
		return utils.RunCmd("systemctl", "restart", "systemd-logind")
	})

	dataDir := Config.DataDir
	backupDir := filepath.Join(baseDir, fmt.Sprintf("backup_%s", Config.InstanceName))

	var pgUserHome string
	executeStep(i18n.T("step_user"), func() error {
		u, err := user.Lookup("postgres")
		if err != nil {
			pgUserHome = filepath.Join(baseDir, "home")
			_ = utils.RunCmd("groupadd", "-g", "5432", "postgres")
			_ = utils.RunCmd("useradd", "-g", "postgres", "-u", "5432", "-d", pgUserHome, "postgres")
			u, _ = user.Lookup("postgres")
		} else {
			pgUserHome = u.HomeDir
		}

		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)

		// Create Directories safely
		dirs := []string{versionPathFull, pgUserHome, dataDir, backupDir}
		for _, d := range dirs {
			os.MkdirAll(d, 0755)
			os.Chown(d, uid, gid)
		}

		// Recursively chown base directory avoiding symlink issues
		filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
			if err == nil {
				os.Chown(path, uid, gid)
			}
			return nil
		})

		return utils.RunCmd("loginctl", "enable-linger", "postgres")
	})

	executeStep(i18n.T("step_env"), func() error {
		bashProfile := filepath.Join(pgUserHome, ".bash_profile")
		pgrcPath := filepath.Join(pgUserHome, ".pgrc")
		u, _ := user.Lookup("postgres")
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)

		bpContent := fmt.Sprintf(`export DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$UID/bus"
alias systemctl='systemctl --user'
source %s
`, pgrcPath)
		os.WriteFile(bashProfile, []byte(bpContent), 0644)
		os.Chown(bashProfile, uid, gid)

		envs := map[string]string{
			"PG_VERSION_PATH":   fmt.Sprintf("'%s'", versionPathFull),
			"PG_RMAN_BACK_PATH": fmt.Sprintf("'%s'", backupDir),
			"PATH":              fmt.Sprintf("'%s/bin':$PATH", versionPathFull),
			"PGDATA":            fmt.Sprintf("'%s'", dataDir),
			"LD_LIBRARY_PATH":   fmt.Sprintf("':%s/lib/'", versionPathFull),
			"PGPORT":            fmt.Sprintf("'%d'", Config.Port),
		}

		if err := utils.UpdatePgrc(pgrcPath, envs); err != nil {
			return err
		}
		os.Chown(pgrcPath, uid, gid)
		return nil
	})

	executeStep(i18n.T("step_initdb"), func() error {
		pgCtl := filepath.Join(versionPathFull, "bin", "pg_ctl")
		cmd := fmt.Sprintf("export LD_LIBRARY_PATH=%s/lib && %s -D %s initdb", versionPathFull, pgCtl, dataDir)
		return utils.RunAsUser("postgres", cmd)
	})

	executeStep(i18n.T("step_pgconf"), func() error {
		confPath := filepath.Join(dataDir, "postgresql.conf")
		utils.ReplaceInFile(confPath, `(?m)^#?logging_collector\s*=.*`, "logging_collector = on")
		utils.ReplaceInFile(confPath, `(?m)^#?password_encryption\s*=.*`, "password_encryption = scram-sha-256")
		utils.ReplaceInFile(confPath, `(?m)^#?listen_addresses\s*=.*`, "listen_addresses = '0.0.0.0'")
		utils.ReplaceInFile(confPath, `(?m)^#?port\s*=.*`, fmt.Sprintf("port = %d", Config.Port))

		hbaPath := filepath.Join(dataDir, "pg_hba.conf")
		return utils.AppendToFile(hbaPath, "\nhost    all             all             0.0.0.0/0          scram-sha-256\n")
	})

	serviceName := fmt.Sprintf("postgresql-%s.service", Config.InstanceName)
	executeStep(i18n.T("step_systemd"), func() error {
		u, _ := user.Lookup("postgres")
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)

		utils.RunAsUser("postgres", "mkdir -p ~/.config/systemd/user")
		sysdDir := filepath.Join(u.HomeDir, ".config", "systemd", "user")
		svcPath := filepath.Join(sysdDir, serviceName)
		pgBin := filepath.Join(versionPathFull, "bin")

		svcContent := fmt.Sprintf(`[Unit]
Description=PostgreSQL database server (%s)
Documentation=man:postgres(1)
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=%s/postgres -D %s
ExecReload=/bin/kill -HUP $MAINPID
KillMode=mixed
KillSignal=SIGINT
TimeoutSec=infinity
Restart=on-failure

[Install]
WantedBy=default.target
`, Config.InstanceName, pgBin, dataDir)

		os.WriteFile(svcPath, []byte(svcContent), 0644)
		os.Chown(svcPath, uid, gid)
		return nil
	})

	executeStep(i18n.T("step_start"), func() error {
		utils.RunAsUser("postgres", "systemctl --user daemon-reload")
		utils.RunAsUser("postgres", fmt.Sprintf("systemctl --user enable %s", serviceName))
		return utils.RunAsUser("postgres", fmt.Sprintf("systemctl --user start %s", serviceName))
	})

	executeStep(i18n.T("step_password"), func() error {
		psql := filepath.Join(versionPathFull, "bin", "psql")
		cmd := fmt.Sprintf("export LD_LIBRARY_PATH=%s/lib && %s -p %d -c \"ALTER USER postgres WITH PASSWORD '%s';\"", versionPathFull, psql, Config.Port, Config.Password)
		return utils.RunAsUser("postgres", cmd)
	})

	// Add to Global Registry
	pgBin = filepath.Join(versionPathFull, "bin", "postgres")
	config.SaveInstanceToRegistry(Config.InstanceName, "postgres", dataDir, pgBin, strconv.Itoa(Config.Port))

	pw.Stop()
	fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
}
