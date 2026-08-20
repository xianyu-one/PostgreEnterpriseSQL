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
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var deploymentPasswordSource string

var installCmd = &cobra.Command{
	Use:     "deploy",
	Aliases: []string{"install"},
	Short:   i18n.T("install_desc"),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareDeployment(cmd); err != nil {
			return err
		}
		return runInstall(cmd)
	},
}

func init() {
	installCmd.Flags().StringVarP(&Config.TarPath, "tar", "t", "postgresql-16.9-x64-Ubuntu24.04.tar.gz", i18n.T("flag_tar"))
	installCmd.Flags().StringVarP(&Config.InstanceName, "instance", "i", "", i18n.T("flag_instance"))
	installCmd.Flags().StringVarP(&Config.OSUser, "os-user", "u", "postgres", i18n.T("flag_os_user"))
	installCmd.Flags().StringVar(&Config.MajorVersion, "major", "16", i18n.T("flag_major"))
	installCmd.Flags().StringVar(&Config.MinorVersion, "minor", "9", i18n.T("flag_minor"))
	installCmd.Flags().StringVar(&Config.DataDir, "data", "", i18n.T("flag_data"))
	installCmd.Flags().IntVarP(&Config.Port, "port", "p", 51721, i18n.T("flag_port"))
	installCmd.Flags().StringVar(&Config.Password, "password", "", i18n.T("flag_password"))
	_ = installCmd.Flags().MarkDeprecated("password", i18n.T("flag_password_deprecated"))
	installCmd.Flags().StringVar(&Config.PasswordEnv, "password-env", "", i18n.T("flag_password_env"))
	installCmd.Flags().StringVar(&Config.PasswordFile, "password-file", "", i18n.T("flag_password_file"))
	installCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, i18n.T("flag_silent_deprecated"))
	_ = installCmd.Flags().MarkDeprecated("silent", i18n.T("flag_silent_replacement"))

	RootCmd.AddCommand(installCmd)
	InstanceCmd.AddCommand(installCmd)
}

func prepareDeployment(cmd *cobra.Command) error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	password, source, err := interaction.ResolveSecret(Config.Password, Config.PasswordEnv, Config.PasswordFile)
	if err != nil {
		return err
	}
	Config.Password = password
	deploymentPasswordSource = source
	if Config.Silent {
		if err := requireExplicitIdentity(true, UI.LegacySilent, &Config.InstanceName, Config.Password); err != nil {
			return err
		}
		if UI.LegacySilent && !cmd.Flags().Changed("instance") {
			fmt.Fprintln(os.Stderr, i18n.T("warn_legacy_default_instance"))
		}
	}
	return nil
}

func runInstall(cmd *cobra.Command) error {
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
			fmt.Fprintln(os.Stderr, i18n.T("version_auto_detected", detectedMajor, detectedMinor))
		} else {
			if len(installed) > 0 {
				selected, selectErr := promptInstalledVersion(i18n.T("prompt_select_version"), installed, len(installed)-1)
				if selectErr != nil {
					return selectErr
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
		if Config.Password == "" {
			Config.Password, err = utils.PromptNewPassword(
				i18n.T("prompt_pass"),
				i18n.T("prompt_pass_confirm"),
				i18n.T("err_password_mismatch"),
			)
			if err != nil {
				return err
			}
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
	if !Config.Silent {
		if deploymentPasswordSource == "" {
			deploymentPasswordSource = i18n.T("secret_source_terminal")
		}
		if err := reviewInstallConfig(i18n.T("review_deploy"), deploymentPasswordSource, true); err != nil {
			return err
		}
	}

	osUser := Config.OSUser
	if osUser == "" {
		osUser = "postgres"
	}

	versionPathFull := filepath.Join(baseDir, Config.MajorVersion, Config.MinorVersion)
	dataDir := Config.DataDir
	backupDir := filepath.Join(baseDir, fmt.Sprintf("backup_%s", Config.InstanceName))

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	operation := interaction.NewOperation(os.Stderr, mode)
	executeStep := func(msg string, action func() error) error {
		return operation.Run(msg, action)
	}

	pgBin := filepath.Join(versionPathFull, "bin", "postgres")
	if _, err := os.Stat(pgBin); err == nil {
		if !Config.Silent {
			overwritePrompt := i18n.T("confirm_overwrite_version", Config.MajorVersion, Config.MinorVersion, versionPathFull)
			if !utils.PromptConfirm(overwritePrompt) {
				fmt.Println(i18n.T("abort"))
				return nil
			}
		}
	}

	var pgUserHome string
	if err := executeStep(i18n.T("step_user"), func() error {
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
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_extract"), func() error {
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
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_env"), func() error {
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
			"PGUSER":            "'postgres'",
			"PGDATABASE":        "'postgres'",
		}

		if err := utils.UpdatePgrc(pgrcPath, envs); err != nil {
			return err
		}
		if err := ensurePgMgrUseShellIntegration(pgrcPath); err != nil {
			return err
		}
		os.Chown(pgrcPath, uid, gid)
		return nil
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_initdb"), func() error {
		pgCtl := filepath.Join(versionPathFull, "bin", "pg_ctl")
		cmd := buildInitDBCommand(versionPathFull, pgCtl, dataDir, "postgres")
		return utils.RunAsUser(osUser, cmd)
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_pgconf"), func() error {
		confPath := filepath.Join(dataDir, "postgresql.conf")
		utils.ReplaceInFile(confPath, `(?m)^#?logging_collector\s*=.*`, "logging_collector = on")
		utils.ReplaceInFile(confPath, `(?m)^#?password_encryption\s*=.*`, "password_encryption = scram-sha-256")
		utils.ReplaceInFile(confPath, `(?m)^#?listen_addresses\s*=.*`, "listen_addresses = '0.0.0.0'")
		utils.ReplaceInFile(confPath, `(?m)^#?port\s*=.*`, fmt.Sprintf("port = %d", Config.Port))

		hbaPath := filepath.Join(dataDir, "pg_hba.conf")
		return utils.AppendToFile(hbaPath, "\nhost    all             all             0.0.0.0/0          scram-sha-256\n")
	}); err != nil {
		return err
	}

	serviceName := fmt.Sprintf("postgresql-%s.service", Config.InstanceName)
	if err := executeStep(i18n.T("step_systemd"), func() error {
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
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_start"), func() error {
		if osUser == "root" {
			utils.RunCmd("systemctl", "daemon-reload")
			utils.RunCmd("systemctl", "enable", serviceName)
			return utils.RunCmd("systemctl", "start", serviceName)
		} else {
			utils.RunAsUser(osUser, "systemctl --user daemon-reload")
			utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user enable %s", serviceName))
			return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user start %s", serviceName))
		}
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_password"), func() error {
		psql := filepath.Join(versionPathFull, "bin", "psql")
		cmd := fmt.Sprintf("export LD_LIBRARY_PATH=%s/lib && %s -p %d -d postgres -U postgres -c \"ALTER USER postgres WITH PASSWORD '%s';\"", versionPathFull, psql, Config.Port, Config.Password)
		return utils.RunAsUser(osUser, cmd)
	}); err != nil {
		return err
	}

	// Add to Global Registry
	pgBin = filepath.Join(versionPathFull, "bin", "postgres")
	if err := config.SaveInstanceToRegistryWithDatabaseConnection(Config.InstanceName, osUser, dataDir, pgBin, strconv.Itoa(Config.Port), "postgres", "postgres"); err != nil {
		return err
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": Config.InstanceName, "data_dir": dataDir, "port": Config.Port, "status": "deployed", "operation": operation.Result()})
	}
	if !UI.Quiet {
		fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
	}
	return nil
}
