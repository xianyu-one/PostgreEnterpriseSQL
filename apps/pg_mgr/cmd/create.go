package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var createInstanceCmd = &cobra.Command{
	Use:     "create",
	Aliases: []string{"create-instance"},
	Short:   i18n.T("create_instance_desc"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCreateInstance(cmd.Flags().Changed("instance"))
	},
}

func init() {
	createInstanceCmd.Flags().StringVarP(&Config.InstanceName, "instance", "i", "", i18n.T("flag_instance"))
	createInstanceCmd.Flags().StringVarP(&Config.OSUser, "os-user", "u", "postgres", i18n.T("flag_os_user"))
	createInstanceCmd.Flags().StringVar(&Config.MajorVersion, "major", "16", i18n.T("flag_major"))
	createInstanceCmd.Flags().StringVar(&Config.MinorVersion, "minor", "9", i18n.T("flag_minor"))
	createInstanceCmd.Flags().StringVar(&Config.DataDir, "data", "", i18n.T("flag_data"))
	createInstanceCmd.Flags().IntVarP(&Config.Port, "port", "p", 51721, i18n.T("flag_port"))
	createInstanceCmd.Flags().StringVar(&Config.Password, "password", "", i18n.T("flag_password"))
	_ = createInstanceCmd.Flags().MarkDeprecated("password", i18n.T("flag_password_deprecated"))
	createInstanceCmd.Flags().StringVar(&Config.PasswordEnv, "password-env", "", i18n.T("flag_password_env"))
	createInstanceCmd.Flags().StringVar(&Config.PasswordFile, "password-file", "", i18n.T("flag_password_file"))
	createInstanceCmd.Flags().StringVar(&Config.DBUser, "db-user", "postgres", i18n.T("flag_db_user"))
	createInstanceCmd.Flags().BoolVar(&Config.SystemctlAlias, "systemctl-alias", false, i18n.T("flag_systemctl_alias"))
	createInstanceCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, i18n.T("flag_silent_deprecated"))
	_ = createInstanceCmd.Flags().MarkDeprecated("silent", i18n.T("flag_silent_replacement"))

	InstanceCmd.AddCommand(createInstanceCmd)
	RootCmd.AddCommand(createInstanceCmd)
}

func runCreateInstance(instanceExplicit bool) error {
	password, passwordSource, err := interaction.ResolveSecret(Config.Password, Config.PasswordEnv, Config.PasswordFile)
	if err != nil {
		return err
	}
	Config.Password = password
	if Config.OSUser == "" {
		Config.OSUser = "postgres"
	}
	if err := utils.CheckUserPermission(Config.OSUser); err != nil {
		return err
	}
	checkRemoveIPC()

	baseDir := config.Global.BaseDir
	installed, err := utils.GetInstalledVersions(baseDir)
	if err == nil && len(installed) > 0 {
		recommended := installed[len(installed)-1]
		Config.MajorVersion = strconv.Itoa(recommended.Major)
		Config.MinorVersion = strconv.Itoa(recommended.Minor)
	}

	if !Config.Silent {
		Config.InstanceName = utils.PromptInput(i18n.T("prompt_inst"), Config.InstanceName)
		Config.OSUser = utils.PromptInput(i18n.T("prompt_os_user"), Config.OSUser)
		if len(installed) > 0 {
			selected, err := promptInstalledVersion(i18n.T("prompt_select_version"), installed, len(installed)-1)
			if err != nil {
				return err
			}
			Config.MajorVersion = strconv.Itoa(selected.Major)
			Config.MinorVersion = strconv.Itoa(selected.Minor)
		}

		defaultDataDir := filepath.Join(baseDir, "instances", Config.InstanceName)
		currentDefault := Config.DataDir
		if currentDefault == "" {
			currentDefault = defaultDataDir
		}
		Config.DataDir = utils.PromptPath(i18n.T("prompt_inst_data_dir"), currentDefault)

		portStr := utils.PromptInput(i18n.T("prompt_port"), strconv.Itoa(Config.Port))
		Config.Port, _ = strconv.Atoi(portStr)
		Config.DBUser = utils.PromptInput(i18n.T("prompt_db_user"), Config.DBUser)
		Config.SystemctlAlias = utils.PromptConfirm(i18n.T("prompt_systemctl_alias"))
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
		if err := requireExplicitIdentity(true, UI.LegacySilent, &Config.InstanceName, Config.Password); err != nil {
			return err
		}
		if UI.LegacySilent && !instanceExplicit {
			fmt.Fprintln(os.Stderr, i18n.T("warn_legacy_default_instance"))
		}
		if Config.DataDir == "" {
			Config.DataDir = filepath.Join(baseDir, "instances", Config.InstanceName)
		}
	}
	if !Config.Silent {
		if passwordSource == "" {
			passwordSource = i18n.T("secret_source_terminal")
		}
		if err := reviewInstallConfig(i18n.T("review_create"), passwordSource, false); err != nil {
			return err
		}
		if err := utils.CheckUserPermission(Config.OSUser); err != nil {
			return err
		}
	}

	osUser := Config.OSUser
	if osUser == "" {
		osUser = "postgres"
	}
	if Config.DBUser == "" {
		Config.DBUser = "postgres"
	}
	if !validDatabaseUser(Config.DBUser) {
		return fmt.Errorf("%s", i18n.T("err_invalid_db_user", Config.DBUser))
	}

	versionPathFull := filepath.Join(baseDir, Config.MajorVersion, Config.MinorVersion)
	pgBin := filepath.Join(versionPathFull, "bin", "postgres")

	// Verify software exists at versionPathFull
	if _, err := os.Stat(pgBin); os.IsNotExist(err) {
		return fmt.Errorf("%s", i18n.T("err_version_not_installed", fmt.Sprintf("%s.%s", Config.MajorVersion, Config.MinorVersion)))
	}

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	operation := interaction.NewOperation(os.Stderr, mode)
	executeStep := func(msg string, action func() error) error {
		return operation.Run(msg, action)
	}

	dataDir := Config.DataDir
	backupDir := filepath.Join(baseDir, fmt.Sprintf("backup_%s", Config.InstanceName))
	_, dataDirStatErr := os.Stat(dataDir)
	_, backupDirStatErr := os.Stat(backupDir)
	dataDirCreated := os.IsNotExist(dataDirStatErr)
	backupDirCreated := os.IsNotExist(backupDirStatErr)
	serviceName := fmt.Sprintf("postgresql-%s.service", Config.InstanceName)
	servicePath := ""
	committed := false
	previousMeta, instanceAlreadyRegistered := config.Global.Instances[Config.InstanceName]
	defer func() {
		if !committed {
			if instanceAlreadyRegistered {
				config.Global.Instances[Config.InstanceName] = previousMeta
			} else {
				delete(config.Global.Instances, Config.InstanceName)
			}
			cleanupFailedCreate(osUser, versionPathFull, dataDir, backupDir, serviceName, servicePath, dataDirCreated, backupDirCreated)
		}
	}()

	var pgUserHome string
	if err := executeStep(i18n.T("step_user"), func() error {
		u, err := user.Lookup(osUser)
		if err != nil {
			if !utils.IsRoot() {
				return fmt.Errorf("user %s does not exist and root privileges are required to create users", osUser)
			}
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

		// Ensure software package directory has proper accessible permissions without altering ownership
		_ = utils.EnsurePkgPermissions(versionPathFull)

		if osUser != "root" {
			return utils.RunCmd("loginctl", "enable-linger", osUser)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_env"), func() error {
		loginShell := lookupLoginShell(osUser)
		profilePath := filepath.Join(pgUserHome, profileFileForShell(loginShell))
		pgrcPath := filepath.Join(pgUserHome, ".pgrc")
		u, _ := user.Lookup(osUser)
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)

		if err := ensureProfileSourcesPgrc(profilePath, pgrcPath); err != nil {
			return err
		}
		_ = os.Chown(profilePath, uid, gid)

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
		if Config.SystemctlAlias {
			if err := ensureSystemctlAlias(pgrcPath); err != nil {
				return err
			}
		}
		os.Chown(pgrcPath, uid, gid)
		return nil
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_initdb"), func() error {
		pgCtl := filepath.Join(versionPathFull, "bin", "pg_ctl")
		cmd := buildInitDBCommand(versionPathFull, pgCtl, dataDir, Config.DBUser)
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
		if _, err := os.Stat(svcPath); err == nil {
			return fmt.Errorf("systemd service already exists: %s", svcPath)
		} else if !os.IsNotExist(err) {
			return err
		}
		servicePath = svcPath

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
		cmd := buildInitialPasswordCommand(versionPathFull, psql, Config.Port, Config.DBUser, Config.Password)
		return utils.RunAsUser(osUser, cmd)
	}); err != nil {
		return err
	}

	// Add to Global Registry
	pgBin = filepath.Join(versionPathFull, "bin", "postgres")
	if err := config.SaveInstanceToRegistryWithDatabaseConnection(Config.InstanceName, osUser, dataDir, pgBin, strconv.Itoa(Config.Port), Config.DBUser, "postgres"); err != nil {
		return err
	}

	committed = true
	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": Config.InstanceName, "data_dir": dataDir, "port": Config.Port, "status": "created", "operation": operation.Result()})
	}
	if !UI.Quiet {
		fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
	}
	return nil
}

func reviewInstallConfig(title, passwordSource string, includeTar bool) error {
	renderer := interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputTable, false)
	prompt := interaction.NewPrompt(os.Stdin, os.Stderr)
	for {
		fields := make([]interaction.ReviewField, 0, 9)
		if includeTar {
			fields = append(fields, interaction.ReviewField{Label: i18n.T("prompt_tar"), Value: Config.TarPath})
		}
		fields = append(fields,
			interaction.ReviewField{Label: i18n.T("tbl_inst"), Value: Config.InstanceName},
			interaction.ReviewField{Label: i18n.T("tbl_ver_version"), Value: Config.MajorVersion + "." + Config.MinorVersion},
			interaction.ReviewField{Label: i18n.T("tbl_datadir"), Value: Config.DataDir},
			interaction.ReviewField{Label: i18n.T("tbl_port"), Value: strconv.Itoa(Config.Port)},
			interaction.ReviewField{Label: i18n.T("tbl_user"), Value: Config.OSUser},
			interaction.ReviewField{Label: i18n.T("tbl_db_user"), Value: Config.DBUser},
			interaction.ReviewField{Label: i18n.T("prompt_pass"), Secret: true, Source: passwordSource},
		)
		renderer.Review(title, fields)
		choice, err := prompt.Menu(i18n.T("prompt_select_option"), []string{i18n.T("review_execute"), i18n.T("review_modify"), i18n.T("menu_cancel")}, 0)
		if err != nil {
			return err
		}
		switch choice {
		case 0:
			return nil
		case 2:
			return interaction.ErrCancelled
		}
		editItems := []string{i18n.T("tbl_inst"), i18n.T("tbl_ver_version"), i18n.T("tbl_datadir"), i18n.T("tbl_port"), i18n.T("tbl_user"), i18n.T("tbl_db_user"), i18n.T("prompt_pass")}
		if includeTar {
			editItems = append([]string{i18n.T("prompt_tar")}, editItems...)
		}
		field, err := prompt.Menu(i18n.T("prompt_modify_field"), editItems, 0)
		if err != nil {
			return err
		}
		if includeTar {
			if field == 0 {
				Config.TarPath = utils.PromptPath(i18n.T("prompt_tar"), Config.TarPath)
				continue
			}
			field--
		}
		switch field {
		case 0:
			Config.InstanceName = utils.PromptInput(i18n.T("prompt_inst"), Config.InstanceName)
		case 1:
			Config.MajorVersion = utils.PromptInput(i18n.T("prompt_major"), Config.MajorVersion)
			Config.MinorVersion = utils.PromptInput(i18n.T("prompt_minor"), Config.MinorVersion)
		case 2:
			Config.DataDir = utils.PromptPath(i18n.T("prompt_inst_data_dir"), Config.DataDir)
		case 3:
			value := utils.PromptInput(i18n.T("prompt_port"), strconv.Itoa(Config.Port))
			port, convErr := strconv.Atoi(value)
			if convErr != nil || port < 1 || port > 65535 {
				return interaction.NewError(interaction.CodeInvalidInput, i18n.T("prompt_port"), interaction.ExitUsage)
			}
			Config.Port = port
		case 4:
			Config.OSUser = utils.PromptInput(i18n.T("prompt_os_user"), Config.OSUser)
		case 5:
			Config.DBUser = utils.PromptInput(i18n.T("prompt_db_user"), Config.DBUser)
		case 6:
			Config.Password, err = utils.PromptNewPassword(i18n.T("prompt_pass"), i18n.T("prompt_pass_confirm"), i18n.T("err_password_mismatch"))
			if err != nil {
				return err
			}
			passwordSource = i18n.T("secret_source_terminal")
		}
	}
}

func buildInitDBCommand(versionPathFull, pgCtl, dataDir, dbUser string) string {
	return fmt.Sprintf("export LD_LIBRARY_PATH=%s/lib && %s -D %s initdb -o \"--username=%s\"",
		versionPathFull, pgCtl, dataDir, dbUser)
}

func buildInitialPasswordCommand(versionPathFull, psql string, port int, dbUser, password string) string {
	password = strings.ReplaceAll(password, "'", "''")
	return fmt.Sprintf("export LD_LIBRARY_PATH=%s/lib && %s -p %d -d postgres -U %s -c \"ALTER USER %s WITH PASSWORD '%s';\"",
		versionPathFull, psql, port, dbUser, dbUser, password)
}

func validDatabaseUser(name string) bool {
	return regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name)
}

func ensureProfileSourcesPgrc(profilePath, pgrcPath string) error {
	sourceLine := fmt.Sprintf("source %s", pgrcPath)
	content, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(content), sourceLine) {
		return nil
	}
	return utils.AppendToFile(profilePath, "\n"+sourceLine+"\n")
}

func ensureSystemctlAlias(pgrcPath string) error {
	const aliasLine = "alias systemctl='systemctl --user'"
	content, err := os.ReadFile(pgrcPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(content), aliasLine) {
		return nil
	}
	return utils.AppendToFile(pgrcPath, "\n"+aliasLine+"\n")
}

func lookupLoginShell(username string) string {
	output, err := exec.Command("getent", "passwd", username).Output()
	if err != nil {
		return ""
	}
	fields := strings.Split(strings.TrimSpace(string(output)), ":")
	if len(fields) < 7 {
		return ""
	}
	return fields[6]
}

func profileFileForShell(loginShell string) string {
	if filepath.Base(loginShell) == "zsh" {
		return ".zshrc"
	}
	return ".bash_profile"
}

func cleanupFailedCreate(osUser, versionPathFull, dataDir, backupDir, serviceName, servicePath string, removeDataDir, removeBackupDir bool) {
	pgCtl := filepath.Join(versionPathFull, "bin", "pg_ctl")
	_ = utils.RunAsUser(osUser, fmt.Sprintf("%s -D %s stop -m immediate", pgCtl, dataDir))

	if osUser == "root" {
		_ = utils.RunCmd("systemctl", "stop", serviceName)
		_ = utils.RunCmd("systemctl", "disable", serviceName)
	} else {
		_ = utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user stop %s", serviceName))
		_ = utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user disable %s", serviceName))
	}
	if servicePath != "" {
		_ = os.Remove(servicePath)
	}
	if osUser == "root" {
		_ = utils.RunCmd("systemctl", "daemon-reload")
	} else {
		_ = utils.RunAsUser(osUser, "systemctl --user daemon-reload")
	}
	if removeDataDir {
		_ = os.RemoveAll(dataDir)
	}
	if removeBackupDir {
		_ = os.RemoveAll(backupDir)
	}
}
