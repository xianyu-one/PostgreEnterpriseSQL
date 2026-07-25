package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var installCmd = &cobra.Command{
	Use:     "deploy",
	Aliases: []string{"install"},
	Short:   i18n.T("install_desc"),
	Run:     func(cmd *cobra.Command, args []string) { runInstall(cmd) },
}

func init() {
	installCmd.Flags().StringVarP(&Config.TarPath, "tar", "t", "postgresql-16.9-x64-Ubuntu24.04.tar.gz", "Path to the tar.gz package")
	installCmd.Flags().StringVarP(&Config.InstanceName, "instance", "i", "default", "Instance name for multi-instance support")
	installCmd.Flags().StringVarP(&Config.OSUser, "os-user", "u", "postgres", "OS user who runs the database instance")
	installCmd.Flags().StringVar(&Config.MajorVersion, "major", "16", "Major version path structure")
	installCmd.Flags().StringVar(&Config.MinorVersion, "minor", "9", "Minor version path structure")
	installCmd.Flags().StringVar(&Config.DataDir, "data", "", "Data directory path (defaults to base_dir/instances/instance_name)")
	installCmd.Flags().IntVarP(&Config.Port, "port", "p", 51721, "Database listener port")
	installCmd.Flags().StringVar(&Config.Password, "password", "SuperSecret123", "Initial password for postgres user")
	installCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, "Run in silent mode without prompts")

	RootCmd.AddCommand(installCmd)
	InstanceCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command) {
	utils.EnsureRoot()
	checkRemoveIPC()

	baseDir := config.Global.BaseDir
	installed, err := utils.GetInstalledVersions(baseDir)
	if err != nil {
		installed = []utils.PGVersion{}
	}

	if !Config.Silent {
		Config.TarPath = utils.PromptPath(i18n.T("prompt_tar"), Config.TarPath)
		Config.InstanceName = utils.PromptInput(i18n.T("prompt_inst"), Config.InstanceName)
		Config.OSUser = utils.PromptInput(i18n.T("prompt_os_user"), Config.OSUser)

		detectedMajor, detectedMinor, detected := utils.DetectVersionFromTar(Config.TarPath)
		if detected {
			Config.MajorVersion = detectedMajor
			Config.MinorVersion = detectedMinor
			fmt.Printf("Auto-detected version from tarball: %s.%s\n", detectedMajor, detectedMinor)
		} else {
			if len(installed) > 0 {
				selected, selectErr := promptInstalledVersion(i18n.T("prompt_select_version"), installed, len(installed)-1)
				if selectErr != nil {
					fmt.Println(text.FgHiRed.Sprint(selectErr))
					return
				}
				Config.MajorVersion = strconv.Itoa(selected.Major)
				Config.MinorVersion = strconv.Itoa(selected.Minor)
			} else {
				Config.MajorVersion = utils.PromptInput(i18n.T("prompt_major"), Config.MajorVersion)
				Config.MinorVersion = utils.PromptInput(i18n.T("prompt_minor"), Config.MinorVersion)
			}
		}

		defaultDataDir := filepath.Join(baseDir, "instances", Config.InstanceName)
		currentDefault := Config.DataDir
		if currentDefault == "" {
			currentDefault = defaultDataDir
		}
		Config.DataDir = utils.PromptPath(i18n.T("prompt_inst_data_dir"), currentDefault)

		portStr := utils.PromptInput(i18n.T("prompt_port"), strconv.Itoa(Config.Port))
		Config.Port, _ = strconv.Atoi(portStr)
		Config.Password, err = utils.PromptNewPassword(
			i18n.T("prompt_pass"),
			i18n.T("prompt_pass_confirm"),
			i18n.T("err_password_mismatch"),
		)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(err))
			return
		}
	} else {
		detectedMajor, detectedMinor, detected := utils.DetectVersionFromTar(Config.TarPath)
		if detected {
			if !cmd.Flags().Changed("major") {
				Config.MajorVersion = detectedMajor
			}
			if !cmd.Flags().Changed("minor") {
				Config.MinorVersion = detectedMinor
			}
		}
		if Config.DataDir == "" {
			Config.DataDir = filepath.Join(baseDir, "instances", Config.InstanceName)
		}
	}

	osUser := Config.OSUser
	if osUser == "" {
		osUser = "postgres"
	}

	versionPathFull := filepath.Join(baseDir, Config.MajorVersion, Config.MinorVersion)
	dataDir := Config.DataDir
	backupDir := filepath.Join(baseDir, fmt.Sprintf("backup_%s", Config.InstanceName))

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

	pgBin := filepath.Join(versionPathFull, "bin", "postgres")
	if _, err := os.Stat(pgBin); err == nil {
		if !Config.Silent {
			overwritePrompt := fmt.Sprintf("Version %s.%s is already installed at %s. Do you want to overwrite it?", Config.MajorVersion, Config.MinorVersion, versionPathFull)
			if !utils.PromptConfirm(overwritePrompt) {
				fmt.Println(i18n.T("abort"))
				return
			}
		}
	}

	var pgUserHome string
	executeStep(i18n.T("step_user"), func() error {
		u, err := user.Lookup(osUser)
		if err != nil {
			pgUserHome = filepath.Join(baseDir, "home", osUser)
			if osUser == "postgres" {
				_ = utils.RunCmd("groupadd", "-g", "5432", "postgres")
				_ = utils.RunCmd("useradd", "-g", "postgres", "-u", "5432", "-d", pgUserHome, "postgres")
			} else {
				_ = utils.RunCmd("groupadd", osUser)
				_ = utils.RunCmd("useradd", "-g", osUser, "-d", pgUserHome, "-m", osUser)
			}
			u, err = user.Lookup(osUser)
			if err != nil {
				return fmt.Errorf("failed to lookup user %s: %v", osUser, err)
			}
		} else {
			pgUserHome = u.HomeDir
		}

		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)

		// Create Directories safely for instance owner
		dirs := []string{pgUserHome, dataDir, backupDir}
		for _, d := range dirs {
			os.MkdirAll(d, 0755)
			os.Chown(d, uid, gid)
		}

		if osUser != "root" {
			return utils.RunCmd("loginctl", "enable-linger", osUser)
		}
		return nil
	})

	executeStep(i18n.T("step_extract"), func() error {
		file, err := os.Open(Config.TarPath)
		if err != nil {
			return err
		}
		defer file.Close()
		u, _ := user.Lookup(osUser)
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		if err := utils.UntarGz(file, versionPathFull, uid, gid); err != nil {
			return err
		}
		return utils.EnsurePkgPermissions(versionPathFull)
	})

	executeStep(i18n.T("step_env"), func() error {
		bashProfile := filepath.Join(pgUserHome, ".bash_profile")
		pgrcPath := filepath.Join(pgUserHome, ".pgrc")
		u, _ := user.Lookup(osUser)
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
		cmd := buildInitDBCommand(versionPathFull, pgCtl, dataDir, "postgres")
		return utils.RunAsUser(osUser, cmd)
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
		u, _ := user.Lookup(osUser)
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)

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

		binDir := filepath.Join(versionPathFull, "bin")

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
WantedBy=%s
`, Config.InstanceName, binDir, dataDir, wantedBy)

		err := os.WriteFile(svcPath, []byte(svcContent), 0644)
		if err != nil {
			return err
		}
		return os.Chown(svcPath, uid, gid)
	})

	executeStep(i18n.T("step_start"), func() error {
		if osUser == "root" {
			utils.RunCmd("systemctl", "daemon-reload")
			utils.RunCmd("systemctl", "enable", serviceName)
			return utils.RunCmd("systemctl", "start", serviceName)
		} else {
			utils.RunAsUser(osUser, "systemctl --user daemon-reload")
			utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user enable %s", serviceName))
			return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user start %s", serviceName))
		}
	})

	executeStep(i18n.T("step_password"), func() error {
		psql := filepath.Join(versionPathFull, "bin", "psql")
		cmd := fmt.Sprintf("export LD_LIBRARY_PATH=%s/lib && %s -p %d -d postgres -U postgres -c \"ALTER USER postgres WITH PASSWORD '%s';\"", versionPathFull, psql, Config.Port, Config.Password)
		return utils.RunAsUser(osUser, cmd)
	})

	// Add to Global Registry
	pgBin = filepath.Join(versionPathFull, "bin", "postgres")
	config.SaveInstanceToRegistryWithDatabaseConnection(Config.InstanceName, osUser, dataDir, pgBin, strconv.Itoa(Config.Port), "postgres", "postgres")

	pw.Stop()
	fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
}
