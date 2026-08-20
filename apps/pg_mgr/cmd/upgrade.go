package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/database"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

type UpgradeConfig struct {
	InstanceName       string
	TargetVersion      string
	Silent             bool
	SkipBackup         bool
	AcceptNoBackupRisk bool
}

var UpgConfig UpgradeConfig
var upgradeEnsureRoot = utils.EnsureRoot

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: i18n.T("upgrade_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runUpgrade() },
}

func init() {
	upgradeCmd.Flags().StringVarP(&UpgConfig.InstanceName, "instance", "i", "", "Instance name to upgrade")
	upgradeCmd.RegisterFlagCompletionFunc("instance", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var list []string
		for name := range config.Global.Instances {
			list = append(list, name)
		}
		return list, cobra.ShellCompDirectiveNoFileComp
	})
	upgradeCmd.Flags().StringVarP(&UpgConfig.TargetVersion, "target-version", "t", "", i18n.T("flag_target_version"))
	upgradeCmd.Flags().BoolVar(&UpgConfig.SkipBackup, "skip-backup", false, i18n.T("flag_skip_upgrade_backup"))
	upgradeCmd.Flags().BoolVar(&UpgConfig.AcceptNoBackupRisk, "accept-no-backup-risk", false, i18n.T("flag_accept_no_backup_risk"))
	upgradeCmd.Flags().BoolVarP(&UpgConfig.Silent, "silent", "s", false, i18n.T("flag_silent_deprecated"))
	_ = upgradeCmd.Flags().MarkDeprecated("silent", i18n.T("flag_silent_replacement"))

	InstanceCmd.AddCommand(upgradeCmd)
	RootCmd.AddCommand(upgradeCmd)
}

func getVersionFromBinPath(baseDir, binPath, osUser string) (utils.PGVersion, error) {
	dir := filepath.Dir(filepath.Dir(binPath))
	rel, err := filepath.Rel(baseDir, dir)
	if err == nil {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) == 2 {
			major, err1 := strconv.Atoi(parts[0])
			minor, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				return utils.PGVersion{Major: major, Minor: minor, Raw: fmt.Sprintf("%d.%d", major, minor)}, nil
			}
		}
	}

	// fallback: run postgres -V
	meta := config.InstanceMeta{BinPath: binPath, User: osUser}
	out, err := utils.RunAsUserWithOutputForInstance(osUser, meta, binPath+" -V")
	if err == nil {
		fields := strings.Fields(out)
		for i, field := range fields {
			if strings.Contains(field, "PostgreSQL") && i+1 < len(fields) {
				verStr := fields[i+1]
				vParts := strings.Split(verStr, ".")
				if len(vParts) >= 2 {
					major, err1 := strconv.Atoi(vParts[0])
					minor, err2 := strconv.Atoi(vParts[1])
					if err1 == nil && err2 == nil {
						return utils.PGVersion{Major: major, Minor: minor, Raw: fmt.Sprintf("%d.%d", major, minor)}, nil
					}
				}
			}
		}
	}

	return utils.PGVersion{}, fmt.Errorf("could not determine version from bin path %s", binPath)
}

func runUpgrade() error {
	if err := upgradeEnsureRoot(); err != nil {
		return err
	}
	if !UpgConfig.Silent {
		selected, err := promptInstance(i18n.T("prompt_select_instance"), nil)
		if err != nil {
			return err
		}
		UpgConfig.InstanceName = selected
	}
	if UpgConfig.Silent && UpgConfig.InstanceName == "" {
		if UI.LegacySilent {
			UpgConfig.InstanceName = "default"
			fmt.Fprintln(os.Stderr, i18n.T("warn_legacy_default_instance"))
		} else {
			return interaction.MissingFlags("--instance")
		}
	}
	if UpgConfig.Silent && !UI.Yes {
		return interaction.MissingFlags("--yes")
	}

	if err := utils.CheckInstancePermission(UpgConfig.InstanceName); err != nil {
		return err
	}

	meta, ok := config.Global.Instances[UpgConfig.InstanceName]
	if !ok {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_not_reg", UpgConfig.InstanceName), interaction.ExitTarget)
	}
	meta, err := recoverAndPersistPgrmanConfig(UpgConfig.InstanceName, meta)
	if err != nil {
		return err
	}
	if UpgConfig.AcceptNoBackupRisk && !UpgConfig.SkipBackup {
		return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_backup_risk_without_skip"), interaction.ExitUsage)
	}
	managedBackup := hasManagedUpgradeBackup(meta)

	baseDir := config.Global.BaseDir
	osUser := meta.User

	currentVer, err := getVersionFromBinPath(baseDir, meta.BinPath, osUser)
	if err != nil {
		return err
	}

	installed, err := utils.GetInstalledVersions(baseDir)
	if err != nil {
		return err
	}

	var candidates []utils.PGVersion
	for _, v := range installed {
		if utils.CompareVersions(v, currentVer) > 0 {
			candidates = append(candidates, v)
		}
	}

	if len(candidates) == 0 {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("upgrade_non_found"), interaction.ExitTarget)
	}

	var targetVer utils.PGVersion
	if UpgConfig.TargetVersion != "" {
		targetVer, err = utils.ParseVersion(UpgConfig.TargetVersion)
		if err != nil {
			return interaction.NewError(interaction.CodeInvalidInput, err.Error(), interaction.ExitUsage).WithCause(err)
		}
		// Validate candidate
		found := false
		for _, c := range candidates {
			if utils.CompareVersions(c, targetVer) == 0 {
				found = true
				break
			}
		}
		if !found {
			// check if target version is lower or same
			targetVerParsed, err2 := utils.ParseVersion(UpgConfig.TargetVersion)
			if err2 == nil {
				cmp := utils.CompareVersions(targetVerParsed, currentVer)
				if cmp == 0 {
					return interaction.NewError(interaction.CodeResourceConflict, i18n.T("err_same_version", UpgConfig.InstanceName, currentVer.Raw), interaction.ExitTarget)
				} else if cmp < 0 {
					return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_lower_version", UpgConfig.InstanceName, currentVer.Raw, UpgConfig.TargetVersion), interaction.ExitUsage)
				}
			}
			return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_version_not_installed", UpgConfig.TargetVersion), interaction.ExitTarget)
		}
	} else {
		// Recommend target version:
		// Minor upgrade (same major) is preferred, highest minor version.
		// Otherwise, highest major version.
		var recommended utils.PGVersion
		hasMinor := false
		for _, c := range candidates {
			if c.Major == currentVer.Major {
				if !hasMinor || c.Minor > recommended.Minor {
					recommended = c
					hasMinor = true
				}
			}
		}
		if !hasMinor {
			// Find overall highest version
			recommended = candidates[len(candidates)-1]
		}

		if UpgConfig.Silent {
			targetVer = recommended
		} else {
			fmt.Fprintln(os.Stderr, text.FgHiCyan.Sprint(i18n.T("upgrade_found", UpgConfig.InstanceName, currentVer.Raw)))
			items := make([]string, 0, len(candidates))
			for _, c := range candidates {
				upgType := i18n.T("upgrade_type_major")
				if c.Major == currentVer.Major {
					upgType = i18n.T("upgrade_type_minor")
				}
				recStr := ""
				if utils.CompareVersions(c, recommended) == 0 {
					recStr = " (" + i18n.T("upgrade_recommended") + ")"
				}
				items = append(items, fmt.Sprintf("PostgreSQL %s — %s%s", c.Raw, upgType, recStr))
			}

			recIdx := 0
			for i, c := range candidates {
				if utils.CompareVersions(c, recommended) == 0 {
					recIdx = i
					break
				}
			}
			idx, err := interaction.NewPrompt(os.Stdin, os.Stderr).Menu(i18n.T("prompt_select_version"), items, recIdx)
			if err != nil {
				return err
			}
			targetVer = candidates[idx]
		}
	}

	// Perform Upgrade
	isMajor := targetVer.Major != currentVer.Major
	newBinPath := filepath.Join(baseDir, strconv.Itoa(targetVer.Major), strconv.Itoa(targetVer.Minor), "bin", "postgres")
	newBinDir := filepath.Join(baseDir, strconv.Itoa(targetVer.Major), strconv.Itoa(targetVer.Minor), "bin")
	oldBinDir := filepath.Dir(meta.BinPath)
	newVersionPathFull := filepath.Join(baseDir, strconv.Itoa(targetVer.Major), strconv.Itoa(targetVer.Minor))
	if isMajor {
		oldDataDirBackup := meta.DataDir + "_old_" + currentVer.Raw
		if err := validateMajorUpgradeWorkspace(meta.DataDir, oldDataDirBackup, currentVer.Major); err != nil {
			return err
		}
		if managedBackup {
			if err := validatePgRmanUpgradeWorkspace(meta.Pgrman.BackupDir, currentVer.Raw); err != nil {
				return err
			}
		}
	}

	u, err := user.Lookup(osUser)
	if err != nil {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_user_not_found", osUser), interaction.ExitTarget).WithCause(err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	var upgradeConnection database.Connection
	if isMajor || managedBackup {
		upgradeConnection, err = database.Resolve(UpgConfig.InstanceName, meta, !UI.NonInteractive)
		if err != nil {
			return err
		}
	}

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	operation := interaction.NewOperation(os.Stderr, mode)
	executeStep := func(msg string, action func() error) error {
		return operation.Run(msg, action)
	}

	serviceName := fmt.Sprintf("postgresql-%s.service", UpgConfig.InstanceName)
	servicePath := filepath.Join(u.HomeDir, ".config", "systemd", "user", serviceName)
	if osUser == "root" {
		servicePath = filepath.Join("/etc/systemd/system", serviceName)
	}
	pgrcPath := filepath.Join(u.HomeDir, ".pgrc")
	var serviceSnapshot, pgrcSnapshot fileSnapshot
	if isMajor {
		serviceSnapshot, err = captureFileSnapshot(servicePath)
		if err != nil {
			return err
		}
		pgrcSnapshot, err = captureFileSnapshot(pgrcPath)
		if err != nil {
			return err
		}
	}
	if managedBackup && UpgConfig.SkipBackup {
		if err := confirmUpgradeWithoutBackup(meta); err != nil {
			return err
		}
	}

	// A managed full backup must complete and validate while the old instance is
	// still running. Explicitly bypassing this guard requires a second,
	// backup-specific acknowledgement in addition to the upgrade confirmation.
	if managedBackup && !UpgConfig.SkipBackup {
		if err := executeStep(i18n.T("step_pre_upgrade_backup"), func() error {
			return runManagedPreUpgradeBackup(meta, upgradeConnection)
		}); err != nil {
			return interaction.NewError(
				interaction.CodeExecutionFailed,
				i18n.T("err_pre_upgrade_backup", err),
				interaction.ExitExecution,
			).WithCause(err)
		}
	}

	// Step 1: Stop Service
	if err := executeStep(i18n.T("step_stop_service"), func() error {
		if osUser == "root" {
			return utils.RunCmd("systemctl", "stop", serviceName)
		}
		return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user stop %s", serviceName))
	}); err != nil {
		return err
	}

	var oldDataDirBackup string
	var newDataDir string
	oldDataDir := meta.DataDir
	oldBackupDir := ""
	oldBackupDirArchived := ""
	backupDirRotated := false
	rollbackUpgrade := func(upgErr error) error { return upgErr }
	if isMajor {
		// Major Upgrade requires pg_upgrade
		oldDataDirBackup = oldDataDir + "_old_" + currentVer.Raw
		newDataDir = oldDataDir

		// Step 2: Backup Data
		if err := executeStep(i18n.T("step_backup_data"), func() error {
			return os.Rename(oldDataDir, oldDataDirBackup)
		}); err != nil {
			return err
		}

		rollbackUpgrade = func(upgErr error) error {
			if UI.Output != string(interaction.OutputJSON) {
				fmt.Fprintf(os.Stderr, "\n%s: %v\n", text.FgHiRed.Sprint(i18n.T("upgrade_rollback")), upgErr)
			}
			if osUser == "root" {
				_ = utils.RunCmd("systemctl", "stop", serviceName)
			} else {
				_ = utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user stop %s", serviceName))
			}
			rollbackErr := restoreMajorUpgradeArtifacts(oldDataDir, oldDataDirBackup, oldBackupDir, oldBackupDirArchived, backupDirRotated)
			configErr := errors.Join(serviceSnapshot.Restore(), pgrcSnapshot.Restore())
			if rollbackErr == nil {
				backupDirRotated = false
			}
			if rollbackErr == nil {
				operation.RolledBack(i18n.T("step_backup_data"))
			}
			var serviceErr error
			if osUser == "root" {
				serviceErr = utils.RunCmd("systemctl", "daemon-reload")
				if serviceErr == nil {
					serviceErr = utils.RunCmd("systemctl", "start", serviceName)
				}
			} else {
				serviceErr = utils.RunAsUser(osUser, "systemctl --user daemon-reload")
				if serviceErr == nil {
					serviceErr = utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user start %s", serviceName))
				}
			}
			if rollbackErr != nil {
				rollbackErr = fmt.Errorf("failed to restore original data directory: %w", rollbackErr)
			}
			if serviceErr != nil {
				serviceErr = fmt.Errorf("failed to restart original service: %w", serviceErr)
			}
			return errors.Join(upgErr, rollbackErr, configErr, serviceErr)
		}

		// Step 3: Initialize new database cluster
		if err := executeStep(i18n.T("step_init_new_db"), func() error {
			oldChecksumsEnabled, err := clusterDataChecksumsEnabled(osUser, oldBinDir, oldDataDirBackup)
			if err != nil {
				return err
			}
			checksumCapabilities, err := detectInitDBChecksumCapabilities(osUser, newBinDir)
			if err != nil {
				return err
			}
			checksumOption, err := initDBChecksumOption(oldChecksumsEnabled, checksumCapabilities)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(newDataDir, 0755); err != nil {
				return err
			}
			if err := os.Chown(newDataDir, uid, gid); err != nil {
				return err
			}
			initDB := filepath.Join(newBinDir, "initdb")
			libraryPath := filepath.Join(newBinDir, "..", "lib")
			cmd := buildUpgradeInitDBCommand(libraryPath, initDB, newDataDir, upgradeConnection.User, checksumOption)
			if err := utils.RunAsUser(osUser, cmd); err != nil {
				return err
			}
			newChecksumsEnabled, err := clusterDataChecksumsEnabled(osUser, newBinDir, newDataDir)
			if err != nil {
				return err
			}
			if err := verifyChecksumStateMatch(oldChecksumsEnabled, newChecksumsEnabled); err != nil {
				return err
			}
			// Reconfigure new cluster configs
			confPath := filepath.Join(newDataDir, "postgresql.conf")
			for _, replacement := range []struct{ pattern, value string }{
				{`(?m)^#?logging_collector\s*=.*`, "logging_collector = on"},
				{`(?m)^#?password_encryption\s*=.*`, "password_encryption = scram-sha-256"},
				{`(?m)^#?listen_addresses\s*=.*`, "listen_addresses = '0.0.0.0'"},
				{`(?m)^#?port\s*=.*`, fmt.Sprintf("port = %s", meta.Port)},
			} {
				if err := utils.ReplaceInFile(confPath, replacement.pattern, replacement.value); err != nil {
					return err
				}
			}

			hbaPath := filepath.Join(newDataDir, "pg_hba.conf")
			return utils.AppendToFile(hbaPath, "\nhost    all             all             0.0.0.0/0          scram-sha-256\n")
		}); err != nil {
			return rollbackUpgrade(err)
		}

		// Stop progress writer to allow user interaction in terminal
		// Run configuration migration wizard if not in silent mode
		if !UpgConfig.Silent {
			runConfigMigrationWizard(oldDataDirBackup, newDataDir)
		}

		// Step 4: Run pg_upgrade
		upgradeDiagnosticDir := filepath.Join(
			filepath.Dir(oldDataDir),
			fmt.Sprintf("pg_upgrade_diagnostics_%s_%s", UpgConfig.InstanceName, time.Now().Format("20060102-150405")),
		)
		if err := executeStep(i18n.T("step_run_pg_upgrade"), func() error {
			if err := os.MkdirAll(upgradeDiagnosticDir, 0755); err != nil {
				return err
			}
			if err := os.Chown(upgradeDiagnosticDir, uid, gid); err != nil {
				return err
			}
			pgUpgradeBin := filepath.Join(newBinDir, "pg_upgrade")
			libraryPath := filepath.Join(newBinDir, "..", "lib") + ":" + filepath.Join(oldBinDir, "..", "lib")
			cmd := buildPgUpgradeCommand(
				upgradeDiagnosticDir,
				libraryPath,
				pgUpgradeBin,
				oldDataDirBackup,
				newDataDir,
				oldBinDir,
				newBinDir,
				upgradeConnection.User,
			)
			if err := runPgUpgradeCommand(osUser, cmd, upgradeDiagnosticDir); err != nil {
				return err
			}
			return os.RemoveAll(upgradeDiagnosticDir)
		}); err != nil {
			operation.Retain(upgradeDiagnosticDir)
			operation.RecoverWith(i18n.T("upgrade_diagnostics_recovery", upgradeDiagnosticDir))
			return rollbackUpgrade(err)
		}

		// Re-initialize pg_rman backup catalog for upgraded database if configured
		if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" && meta.Pgrman.BackupDir != "" {
			if err := executeStep(i18n.T("step_reinit_pgrman"), func() (stepErr error) {
				oldBackupDir = filepath.Clean(meta.Pgrman.BackupDir)
				archiveLogPath := pgrmanArchiveLogPath(meta)
				defer func() {
					if stepErr != nil && backupDirRotated {
						if restoreErr := restorePgRmanBackupDirectory(oldBackupDir, oldBackupDirArchived); restoreErr != nil {
							stepErr = errors.Join(stepErr, fmt.Errorf("failed to restore original pg_rman backup directory: %w", restoreErr))
						} else {
							backupDirRotated = false
						}
					}
				}()
				if oldBackupDir != "" {
					if _, err := os.Stat(oldBackupDir); err == nil {
						oldBackupDirArchived = archivedBackupDirectory(oldBackupDir, currentVer.Raw)
						if _, err := os.Stat(oldBackupDirArchived); err == nil {
							return fmt.Errorf("pg_rman recovery directory already exists: %s", oldBackupDirArchived)
						} else if !os.IsNotExist(err) {
							return err
						}
						if err := os.Rename(oldBackupDir, oldBackupDirArchived); err != nil {
							return fmt.Errorf("failed to rename old pg_rman backup directory: %w", err)
						}
						backupDirRotated = true
					}

					if err := os.MkdirAll(oldBackupDir, 0755); err != nil {
						return fmt.Errorf("failed to create new pg_rman backup directory: %w", err)
					}
					if err := os.Chown(oldBackupDir, uid, gid); err != nil {
						return err
					}

					if archiveLogPath != "" {
						if err := os.MkdirAll(archiveLogPath, 0755); err != nil {
							return err
						}
						if err := os.Chown(archiveLogPath, uid, gid); err != nil {
							return err
						}
					}

					pgrmanBin := filepath.Join(newBinDir, "pg_rman")
					if _, err := os.Stat(pgrmanBin); err != nil {
						pgrmanBin = getPgrmanBin(meta)
					}

					upgMeta := meta
					upgMeta.DataDir = newDataDir
					upgMeta.BinPath = filepath.Join(newBinDir, "postgres")

					if err := runPgRmanInit(osUser, upgMeta, pgrmanBin, oldBackupDir); err != nil {
						return err
					}

					iniPath := filepath.Join(oldBackupDir, "pg_rman.ini")
					iniContent := fmt.Sprintf("SRVLOG_PATH='%s'\nARCLOG_PATH='%s'\nCOMPRESS_DATA=%s\nKEEP_ARCLOG_DAYS=%d\nKEEP_SRVLOG_DAYS=%d\nKEEP_DATA_DAYS=%d\n",
						meta.Pgrman.SrvLogPath, archiveLogPath, meta.Pgrman.CompressData,
						meta.Pgrman.KeepArcLogDays, meta.Pgrman.KeepSrvLogDays, meta.Pgrman.KeepDataDays)

					if err := os.WriteFile(iniPath, []byte(iniContent), 0644); err != nil {
						return fmt.Errorf("failed to write pg_rman.ini: %w", err)
					}
					if err := os.Chown(iniPath, uid, gid); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return rollbackUpgrade(err)
			}
		}
	}

	// Step 5: Update systemd service file
	if err := executeStep(i18n.T("step_update_systemd"), func() error {
		var wantedBy string
		if osUser == "root" {
			wantedBy = "multi-user.target"
		} else {
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
`, UpgConfig.InstanceName, newBinPath, meta.DataDir, wantedBy)

		if err := os.WriteFile(servicePath, []byte(svcContent), 0644); err != nil {
			return err
		}
		return os.Chown(servicePath, uid, gid)
	}); err != nil {
		return rollbackUpgrade(err)
	}

	// Step 6: Update user environment (.pgrc)
	if err := executeStep(i18n.T("step_update_env"), func() error {
		backupDir := filepath.Join(baseDir, fmt.Sprintf("backup_%s", UpgConfig.InstanceName))
		databaseUser := meta.DatabaseUser
		if databaseUser == "" {
			databaseUser = "postgres"
		}
		databaseName := meta.DatabaseName
		if databaseName == "" {
			databaseName = "postgres"
		}
		if meta.Pgrman != nil && meta.Pgrman.BackupDir != "" {
			backupDir = meta.Pgrman.BackupDir
		}

		envs := map[string]string{
			"PG_VERSION_PATH":   fmt.Sprintf("'%s'", newVersionPathFull),
			"PG_RMAN_BACK_PATH": fmt.Sprintf("'%s'", backupDir),
			"PATH":              fmt.Sprintf("'%s/bin':$PATH", newVersionPathFull),
			"PGDATA":            fmt.Sprintf("'%s'", meta.DataDir),
			"LD_LIBRARY_PATH":   fmt.Sprintf("':%s/lib/'", newVersionPathFull),
			"PGPORT":            fmt.Sprintf("'%s'", meta.Port),
			"PGUSER":            fmt.Sprintf("'%s'", databaseUser),
			"PGDATABASE":        fmt.Sprintf("'%s'", databaseName),
		}

		if err := utils.UpdatePgrc(pgrcPath, envs); err != nil {
			return err
		}
		if err := ensurePgMgrUseShellIntegration(pgrcPath); err != nil {
			return err
		}
		return os.Chown(pgrcPath, uid, gid)
	}); err != nil {
		return rollbackUpgrade(err)
	}

	// Step 7: Start service
	if err := executeStep(i18n.T("step_start_service"), func() error {
		if osUser == "root" {
			if err := utils.RunCmd("systemctl", "daemon-reload"); err != nil {
				return err
			}
			return utils.RunCmd("systemctl", "start", serviceName)
		}
		if err := utils.RunAsUser(osUser, "systemctl --user daemon-reload"); err != nil {
			return err
		}
		if err := utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user start %s", serviceName)); err != nil {
			return fmt.Errorf("%w\n%s", err, i18n.T("upgrade_service_diagnostic", osUser, osUser, serviceName, osUser, serviceName))
		}
		return nil
	}); err != nil {
		return rollbackUpgrade(err)
	}

	// Step 8: Update registry
	if err := executeStep(i18n.T("step_update_registry"), func() error {
		return config.SaveInstanceToRegistry(UpgConfig.InstanceName, osUser, meta.DataDir, newBinPath, meta.Port)
	}); err != nil {
		return rollbackUpgrade(err)
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": UpgConfig.InstanceName, "target_version": targetVer.Raw, "status": "upgraded", "operation": operation.Result()})
	}
	if !UI.Quiet {
		fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("upgrade_success", UpgConfig.InstanceName, targetVer.Raw)))
		fmt.Println(text.FgHiYellow.Sprint(i18n.T("upgrade_collation_notice")))
	}
	return nil
}

func runPgUpgradeCommand(osUser, cmd, diagnosticDir string) error {
	if err := utils.RunAsUser(osUser, cmd); err != nil {
		return interaction.NewError(
			interaction.CodeExecutionFailed,
			i18n.T("err_pg_upgrade_failed", err, diagnosticDir),
			interaction.ExitExecution,
		).WithCause(err).WithDetail("diagnostic_directory", diagnosticDir)
	}
	return nil
}

func buildPgUpgradeCommand(diagnosticDir, libraryPath, pgUpgradeBin, oldDataDir, newDataDir, oldBinDir, newBinDir, databaseUser string) string {
	return fmt.Sprintf("cd %s && export LD_LIBRARY_PATH=%s && %s -d %s -D %s -b %s -B %s -U %s",
		shellQuote(diagnosticDir),
		shellQuote(libraryPath),
		shellQuote(pgUpgradeBin),
		shellQuote(oldDataDir),
		shellQuote(newDataDir),
		shellQuote(oldBinDir),
		shellQuote(newBinDir),
		shellQuote(databaseUser))
}

func buildUpgradeInitDBCommand(libraryPath, initDB, dataDir, databaseUser, checksumOption string) string {
	command := fmt.Sprintf("export LD_LIBRARY_PATH=%s && %s -D %s -U %s",
		shellQuote(libraryPath), shellQuote(initDB), shellQuote(dataDir), shellQuote(databaseUser))
	if checksumOption != "" {
		command += " " + checksumOption
	}
	return command
}

func archivedBackupDirectory(backupDir, version string) string {
	return filepath.Clean(backupDir) + "_old_" + version
}

func validateMajorUpgradeWorkspace(dataDir, recoveryDir string, expectedMajor int) error {
	if _, err := os.Stat(recoveryDir); err == nil {
		return interaction.NewError(
			interaction.CodeResourceConflict,
			i18n.T("err_upgrade_recovery_dir_exists", recoveryDir),
			interaction.ExitTarget,
		)
	} else if !os.IsNotExist(err) {
		return err
	}
	content, err := os.ReadFile(filepath.Join(dataDir, "PG_VERSION"))
	if err != nil {
		return fmt.Errorf("failed to read source cluster PG_VERSION: %w", err)
	}
	majorText := strings.Split(strings.TrimSpace(string(content)), ".")[0]
	major, err := strconv.Atoi(majorText)
	if err != nil || major != expectedMajor {
		return interaction.NewError(
			interaction.CodeResourceConflict,
			i18n.T("err_upgrade_data_version_mismatch", expectedMajor, dataDir, strings.TrimSpace(string(content))),
			interaction.ExitTarget,
		)
	}
	return nil
}

func validatePgRmanUpgradeWorkspace(backupDir, version string) error {
	archivedDir := archivedBackupDirectory(backupDir, version)
	if _, err := os.Stat(archivedDir); err == nil {
		return interaction.NewError(
			interaction.CodeResourceConflict,
			i18n.T("err_upgrade_pgrman_recovery_dir_exists", archivedDir, filepath.Clean(backupDir)),
			interaction.ExitTarget,
		)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func restoreMajorUpgradeDataDirectory(dataDir, recoveryDir string) error {
	if _, err := os.Stat(recoveryDir); err != nil {
		return fmt.Errorf("recovery directory %s is unavailable: %w", recoveryDir, err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		return err
	}
	return os.Rename(recoveryDir, dataDir)
}

func restorePgRmanBackupDirectory(backupDir, archivedDir string) error {
	if archivedDir == "" {
		return nil
	}
	if err := os.RemoveAll(backupDir); err != nil {
		return err
	}
	return os.Rename(archivedDir, backupDir)
}

func restoreMajorUpgradeArtifacts(dataDir, oldDataDir, backupDir, oldBackupDir string, backupRotated bool) error {
	var backupErr error
	if backupRotated {
		backupErr = restorePgRmanBackupDirectory(backupDir, oldBackupDir)
	}
	dataErr := restoreMajorUpgradeDataDirectory(dataDir, oldDataDir)
	return errors.Join(backupErr, dataErr)
}

type fileSnapshot struct {
	path    string
	content []byte
	mode    os.FileMode
	existed bool
}

func captureFileSnapshot(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fileSnapshot{path: path}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{path: path, content: content, mode: info.Mode().Perm(), existed: true}, nil
}

func (snapshot fileSnapshot) Restore() error {
	if snapshot.path == "" {
		return nil
	}
	if !snapshot.existed {
		if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(snapshot.path, snapshot.content, snapshot.mode)
}

var runUpgradeCommandAsUser = utils.RunAsUserWithCombinedOutput

func runPgRmanInit(osUser string, meta config.InstanceMeta, pgrmanBin, backupDir string) error {
	initCommand := fmt.Sprintf("%s init -B %s -D %s",
		shellQuote(pgrmanBin), shellQuote(backupDir), shellQuote(meta.DataDir))
	command := utils.BuildInstanceCmd(meta, initCommand)
	output, err := runUpgradeCommandAsUser(osUser, command)
	if err != nil && !strings.Contains(strings.ToLower(output), "already initialized") {
		return fmt.Errorf("pg_rman init failed: %s", strings.TrimSpace(output))
	}
	return nil
}

func clusterDataChecksumsEnabled(osUser, oldBinDir, dataDir string) (bool, error) {
	pgControlData := filepath.Join(oldBinDir, "pg_controldata")
	cmd := fmt.Sprintf("LC_ALL=C %s %s", shellQuote(pgControlData), shellQuote(dataDir))
	output, err := utils.RunAsUserWithCombinedOutput(osUser, cmd)
	if err != nil {
		return false, interaction.NewError(
			interaction.CodeExecutionFailed,
			i18n.T("err_checksum_detection", err, strings.TrimSpace(output)),
			interaction.ExitExecution,
		).WithCause(err)
	}
	return parseDataChecksumState(output)
}

func parseDataChecksumState(output string) (bool, error) {
	const label = "Data page checksum version:"
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, label) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, label))
		version, err := strconv.Atoi(value)
		if err != nil {
			return false, interaction.NewError(
				interaction.CodeExecutionFailed,
				i18n.T("err_checksum_value", value),
				interaction.ExitExecution,
			).WithCause(err)
		}
		return version > 0, nil
	}
	return false, interaction.NewError(
		interaction.CodeExecutionFailed,
		i18n.T("err_checksum_missing"),
		interaction.ExitExecution,
	)
}

type initDBChecksumCapabilities struct {
	Enable  bool
	Disable bool
}

func detectInitDBChecksumCapabilities(osUser, binDir string) (initDBChecksumCapabilities, error) {
	initDB := filepath.Join(binDir, "initdb")
	libraryPath := filepath.Join(binDir, "..", "lib")
	cmd := fmt.Sprintf("LC_ALL=C LD_LIBRARY_PATH=%s %s --help", shellQuote(libraryPath), shellQuote(initDB))
	output, err := utils.RunAsUserWithCombinedOutput(osUser, cmd)
	if err != nil {
		return initDBChecksumCapabilities{}, interaction.NewError(
			interaction.CodeExecutionFailed,
			i18n.T("err_initdb_capabilities", err, strings.TrimSpace(output)),
			interaction.ExitExecution,
		).WithCause(err)
	}
	return parseInitDBChecksumCapabilities(output), nil
}

func parseInitDBChecksumCapabilities(help string) initDBChecksumCapabilities {
	capabilities := initDBChecksumCapabilities{}
	for _, field := range strings.Fields(help) {
		switch strings.TrimSpace(field) {
		case "--data-checksums":
			capabilities.Enable = true
		case "--no-data-checksums":
			capabilities.Disable = true
		}
	}
	return capabilities
}

func initDBChecksumOption(oldChecksumsEnabled bool, capabilities initDBChecksumCapabilities) (string, error) {
	if oldChecksumsEnabled {
		if !capabilities.Enable {
			return "", interaction.NewError(
				interaction.CodeExecutionFailed,
				i18n.T("err_initdb_enable_checksums_unsupported"),
				interaction.ExitExecution,
			)
		}
		return "--data-checksums", nil
	}
	if capabilities.Disable {
		return "--no-data-checksums", nil
	}
	// Older initdb versions defaulted to disabled checksums and had no explicit
	// disable flag. The post-init pg_controldata check below validates the actual
	// state instead of trusting that historical behavior.
	return "", nil
}

func verifyChecksumStateMatch(oldEnabled, newEnabled bool) error {
	if oldEnabled == newEnabled {
		return nil
	}
	return interaction.NewError(
		interaction.CodeResourceConflict,
		i18n.T("err_checksum_state_mismatch", checksumState(oldEnabled), checksumState(newEnabled)),
		interaction.ExitTarget,
	)
}

func checksumState(enabled bool) string {
	if enabled {
		return i18n.T("checksum_enabled")
	}
	return i18n.T("checksum_disabled")
}

func hasManagedUpgradeBackup(meta config.InstanceMeta) bool {
	return meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" && strings.TrimSpace(meta.Pgrman.BackupDir) != ""
}

func confirmUpgradeWithoutBackup(meta config.InstanceMeta) error {
	if UI.NonInteractive {
		if !UpgConfig.AcceptNoBackupRisk {
			return interaction.MissingFlags("--accept-no-backup-risk")
		}
		return nil
	}
	fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("warn_skip_upgrade_backup")))
	fmt.Fprintln(os.Stderr, i18n.T("skip_backup_instance", UpgConfig.InstanceName))
	fmt.Fprintln(os.Stderr, i18n.T("skip_backup_catalog", meta.Pgrman.BackupDir))
	fmt.Fprintln(os.Stderr, i18n.T("skip_backup_risk"))
	choice, err := interaction.NewPrompt(os.Stdin, os.Stderr).Menu(
		i18n.T("confirm_skip_upgrade_backup"),
		[]string{i18n.T("option_yes"), i18n.T("option_no")},
		1,
	)
	if err != nil {
		return err
	}
	if choice != 0 {
		return interaction.ErrCancelled
	}
	return nil
}

func runManagedPreUpgradeBackup(meta config.InstanceMeta, connection database.Connection) error {
	if pgrmanArchiveLogPath(meta) == "" {
		return interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_pgrman_archive_path_missing"), interaction.ExitTarget)
	}
	command := buildPgRmanBackupCommand(meta, "full", connection)
	execCommand := utils.BuildInstanceCmd(meta, command)
	writer := io.Writer(io.Discard)
	if UI.Output != string(interaction.OutputJSON) {
		writer = os.Stderr
	}
	output, err := utils.RunAsUserWithLiveOutput(meta.User, execCommand, writer)
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(output))
	}
	return nil
}

type ConfigParam struct {
	Name  string
	Value string
}

func parseConfValue(valPart string) string {
	valPart = strings.TrimSpace(valPart)
	if valPart == "" {
		return ""
	}
	if strings.HasPrefix(valPart, "'") {
		var sb strings.Builder
		sb.WriteByte('\'')
		runes := []rune(valPart)
		i := 1
		for i < len(runes) {
			if runes[i] == '\'' {
				if i+1 < len(runes) && runes[i+1] == '\'' {
					sb.WriteString("''")
					i += 2
					continue
				}
				sb.WriteRune('\'')
				break
			} else {
				sb.WriteRune(runes[i])
				i++
			}
		}
		return sb.String()
	}
	idx := strings.Index(valPart, "#")
	if idx != -1 {
		return strings.TrimSpace(valPart[:idx])
	}
	return valPart
}

func parsePostgresqlConf(content string) []ConfigParam {
	var params []ConfigParam
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.Index(trimmed, "=")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		valPart := trimmed[idx+1:]
		val := parseConfValue(valPart)
		if key != "" {
			params = append(params, ConfigParam{Name: key, Value: val})
		}
	}
	return params
}

func getConfParamsMap(filePath string) (map[string]string, error) {
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	params := parsePostgresqlConf(string(bytes))
	m := make(map[string]string)
	for _, p := range params {
		m[p.Name] = p.Value
	}
	return m, nil
}

func updatePostgresqlConfParam(filePath string, name string, val string) error {
	return utils.UpdatePostgresqlConfParam(filePath, name, val)
}

func parsePgHbaConf(content string) []string {
	var rules []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		rules = append(rules, trimmed)
	}
	return rules
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func isRulePresent(newRules []string, oldRule string) bool {
	normOld := normalizeSpace(oldRule)
	for _, r := range newRules {
		if normalizeSpace(r) == normOld {
			return true
		}
	}
	return false
}

func runConfigMigrationWizard(oldDataDir, newDataDir string) {
	oldConfPath := filepath.Join(oldDataDir, "postgresql.conf")
	newConfPath := filepath.Join(newDataDir, "postgresql.conf")
	oldHbaPath := filepath.Join(oldDataDir, "pg_hba.conf")
	newHbaPath := filepath.Join(newDataDir, "pg_hba.conf")

	// 1. postgresql.conf
	oldParams, err := getConfParamsMap(oldConfPath)
	if err == nil {
		newParams, err := getConfParamsMap(newConfPath)
		if err == nil {
			var names []string
			for k := range oldParams {
				names = append(names, k)
			}
			sort.Strings(names)

			firstParamHeader := false
			for _, name := range names {
				oldVal := oldParams[name]
				newVal, exists := newParams[name]

				// Skip if the new config already has the exact same uncommented value
				if exists && newVal == oldVal {
					continue
				}

				if !firstParamHeader {
					fmt.Println(text.FgHiCyan.Sprint("\n--- PostgreSQL Configuration Migration Wizard (postgresql.conf) ---"))
					firstParamHeader = true
				}

				newValDisplay := newVal
				if !exists {
					newValDisplay = "[Not Set / Default]"
				}

				fmt.Printf("\nParameter: %s\n", text.FgHiYellow.Sprint(name))
				fmt.Printf("  Old value: %s\n", text.FgGreen.Sprint(oldVal))
				fmt.Printf("  New value: %s\n", text.FgRed.Sprint(newValDisplay))
				fmt.Println(i18n.T("migrate_prompt_options"))

				var choice string
				for {
					choice = utils.PromptInput(i18n.T("migrate_prompt_choice"), "1")
					if choice == "1" || choice == "2" || choice == "3" {
						break
					}
					fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_choice")))
				}

				switch choice {
				case "1":
					err := updatePostgresqlConfParam(newConfPath, name, oldVal)
					if err != nil {
						fmt.Printf("Error updating parameter: %v\n", err)
					} else {
						fmt.Println(text.FgGreen.Sprint(i18n.T("migrate_success_old", name)))
					}
				case "2":
					fmt.Println(text.FgGreen.Sprint(i18n.T("migrate_success_new", name)))
				case "3":
					customVal := utils.PromptInput(i18n.T("migrate_prompt_custom", name), "")
					err := updatePostgresqlConfParam(newConfPath, name, customVal)
					if err != nil {
						fmt.Printf("Error updating parameter: %v\n", err)
					} else {
						fmt.Println(text.FgGreen.Sprint(i18n.T("migrate_success_custom", name)))
					}
				}
			}
		}
	}

	// 2. pg_hba.conf
	oldHbaContent, err := os.ReadFile(oldHbaPath)
	if err == nil {
		newHbaContent, err := os.ReadFile(newHbaPath)
		if err == nil {
			oldRules := parsePgHbaConf(string(oldHbaContent))
			newRules := parsePgHbaConf(string(newHbaContent))

			firstHbaHeader := false
			for _, rule := range oldRules {
				if isRulePresent(newRules, rule) {
					continue
				}

				if !firstHbaHeader {
					fmt.Println(text.FgHiCyan.Sprint("\n--- Client Authentication Migration Wizard (pg_hba.conf) ---"))
					firstHbaHeader = true
				}

				fmt.Printf("\nHBA Rule: %s\n", text.FgHiYellow.Sprint(rule))
				fmt.Printf("  Old rule: %s\n", text.FgGreen.Sprint(rule))
				fmt.Printf("  New rule: %s\n", text.FgRed.Sprint("[Not found]"))
				fmt.Println(i18n.T("migrate_prompt_options"))

				var choice string
				for {
					choice = utils.PromptInput(i18n.T("migrate_prompt_choice"), "1")
					if choice == "1" || choice == "2" || choice == "3" {
						break
					}
					fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_choice")))
				}

				switch choice {
				case "1":
					err := utils.AppendToFile(newHbaPath, fmt.Sprintf("\n%s\n", rule))
					if err != nil {
						fmt.Printf("Error migrating HBA rule: %v\n", err)
					} else {
						fmt.Println(text.FgGreen.Sprint(i18n.T("migrate_hba_success_old", rule)))
						newRules = append(newRules, rule)
					}
				case "2":
					fmt.Println(text.FgGreen.Sprint(i18n.T("migrate_hba_success_new")))
				case "3":
					customRule := utils.PromptInput(i18n.T("migrate_hba_prompt_custom", rule), rule)
					if strings.TrimSpace(customRule) != "" {
						err := utils.AppendToFile(newHbaPath, fmt.Sprintf("\n%s\n", customRule))
						if err != nil {
							fmt.Printf("Error migrating custom HBA rule: %v\n", err)
						} else {
							fmt.Println(text.FgGreen.Sprint(i18n.T("migrate_hba_success_custom", customRule)))
							newRules = append(newRules, customRule)
						}
					}
				}
			}
		}
	}
}
