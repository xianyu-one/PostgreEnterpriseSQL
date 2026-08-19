package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

type UpgradeConfig struct {
	InstanceName  string
	TargetVersion string
	Silent        bool
}

var UpgConfig UpgradeConfig

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
			fmt.Println(text.FgHiCyan.Sprint(i18n.T("upgrade_found", UpgConfig.InstanceName, currentVer.Raw)))
			t := table.NewWriter()
			t.SetOutputMirror(os.Stdout)
			t.AppendHeader(table.Row{"ID", "Version", "Upgrade Type", "Recommended"})
			for i, c := range candidates {
				upgType := "Major Upgrade"
				if c.Major == currentVer.Major {
					upgType = "Minor Upgrade"
				}
				recStr := ""
				if utils.CompareVersions(c, recommended) == 0 {
					recStr = "[Recommended]"
				}
				t.AppendRow(table.Row{i + 1, c.Raw, upgType, recStr})
			}
			t.Render()

			recIdx := 0
			for i, c := range candidates {
				if utils.CompareVersions(c, recommended) == 0 {
					recIdx = i + 1
					break
				}
			}

			idxStr := utils.PromptInput(i18n.T("prompt_upgrade_idx"), strconv.Itoa(recIdx))
			idx, err := strconv.Atoi(idxStr)
			if err != nil || idx < 1 || idx > len(candidates) {
				if idx == 0 {
					fmt.Println(i18n.T("abort"))
				} else {
					fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_id")))
				}
				return interaction.ErrCancelled
			}
			targetVer = candidates[idx-1]
		}
	}

	// Perform Upgrade
	isMajor := targetVer.Major != currentVer.Major
	newBinPath := filepath.Join(baseDir, strconv.Itoa(targetVer.Major), strconv.Itoa(targetVer.Minor), "bin", "postgres")
	newBinDir := filepath.Join(baseDir, strconv.Itoa(targetVer.Major), strconv.Itoa(targetVer.Minor), "bin")
	oldBinDir := filepath.Dir(meta.BinPath)
	newVersionPathFull := filepath.Join(baseDir, strconv.Itoa(targetVer.Major), strconv.Itoa(targetVer.Minor))

	u, err := user.Lookup(osUser)
	if err != nil {
		return interaction.NewError(interaction.CodeTargetNotFound, i18n.T("err_user_not_found", osUser), interaction.ExitTarget).WithCause(err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	operation := interaction.NewOperation(os.Stderr, mode)
	executeStep := func(msg string, action func() error) error {
		return operation.Run(msg, action)
	}

	serviceName := fmt.Sprintf("postgresql-%s.service", UpgConfig.InstanceName)

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
	if isMajor {
		// Major Upgrade requires pg_upgrade
		oldDataDir := meta.DataDir
		oldDataDirBackup = oldDataDir + "_old_" + currentVer.Raw
		newDataDir = oldDataDir

		// Clean up any left over backup directory if it exists
		_ = os.RemoveAll(oldDataDirBackup)

		// Step 2: Backup Data
		if err := executeStep(i18n.T("step_backup_data"), func() error {
			return os.Rename(oldDataDir, oldDataDirBackup)
		}); err != nil {
			return err
		}

		rollback := func(upgErr error) error {
			if UI.Output != string(interaction.OutputJSON) {
				fmt.Fprintf(os.Stderr, "\n%s: %v\n", text.FgHiRed.Sprint(i18n.T("upgrade_rollback")), upgErr)
			}
			_ = os.RemoveAll(newDataDir)
			_ = os.Rename(oldDataDirBackup, oldDataDir)
			operation.RolledBack(i18n.T("step_backup_data"))
			if osUser == "root" {
				_ = utils.RunCmd("systemctl", "start", serviceName)
			} else {
				_ = utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user start %s", serviceName))
			}
			return upgErr
		}

		// Step 3: Initialize new database cluster
		if err := executeStep(i18n.T("step_init_new_db"), func() error {
			if err := os.MkdirAll(newDataDir, 0755); err != nil {
				return err
			}
			os.Chown(newDataDir, uid, gid)
			pgCtl := filepath.Join(newBinDir, "pg_ctl")
			cmd := fmt.Sprintf("export LD_LIBRARY_PATH=%s/../lib && %s -D %s initdb", newBinDir, pgCtl, newDataDir)
			if err := utils.RunAsUser(osUser, cmd); err != nil {
				return err
			}
			// Reconfigure new cluster configs
			confPath := filepath.Join(newDataDir, "postgresql.conf")
			_ = utils.ReplaceInFile(confPath, `(?m)^#?logging_collector\s*=.*`, "logging_collector = on")
			_ = utils.ReplaceInFile(confPath, `(?m)^#?password_encryption\s*=.*`, "password_encryption = scram-sha-256")
			_ = utils.ReplaceInFile(confPath, `(?m)^#?listen_addresses\s*=.*`, "listen_addresses = '0.0.0.0'")
			_ = utils.ReplaceInFile(confPath, `(?m)^#?port\s*=.*`, fmt.Sprintf("port = %s", meta.Port))

			hbaPath := filepath.Join(newDataDir, "pg_hba.conf")
			_ = utils.AppendToFile(hbaPath, "\nhost    all             all             0.0.0.0/0          scram-sha-256\n")
			return nil
		}); err != nil {
			return rollback(err)
		}

		// Stop progress writer to allow user interaction in terminal
		// Run configuration migration wizard if not in silent mode
		if !UpgConfig.Silent {
			runConfigMigrationWizard(oldDataDirBackup, newDataDir)
		}

		// Step 4: Run pg_upgrade
		if err := executeStep(i18n.T("step_run_pg_upgrade"), func() error {
			pgUpgradeBin := filepath.Join(newBinDir, "pg_upgrade")
			// Run pg_upgrade in the home directory to ensure write access to log files
			cmd := fmt.Sprintf("cd ~ && export LD_LIBRARY_PATH=%s/../lib:%s/../lib && %s -d %s -D %s -b %s -B %s",
				newBinDir, oldBinDir, pgUpgradeBin, oldDataDirBackup, newDataDir, oldBinDir, newBinDir)
			if err := utils.RunAsUser(osUser, cmd); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return rollback(err)
		}

		// Re-initialize pg_rman backup catalog for upgraded database if configured
		if meta.Pgrman != nil && meta.Pgrman.Tool == "pgrman" && meta.Pgrman.BackupDir != "" {
			if err := executeStep(i18n.T("step_reinit_pgrman"), func() error {
				oldBackupDir := meta.Pgrman.BackupDir
				if oldBackupDir != "" {
					if _, err := os.Stat(oldBackupDir); err == nil {
						oldBackupDirArchived := oldBackupDir + "_old_" + currentVer.Raw
						_ = os.RemoveAll(oldBackupDirArchived)
						if err := os.Rename(oldBackupDir, oldBackupDirArchived); err != nil {
							return fmt.Errorf("failed to rename old pg_rman backup directory: %w", err)
						}
					}

					if err := os.MkdirAll(oldBackupDir, 0755); err != nil {
						return fmt.Errorf("failed to create new pg_rman backup directory: %w", err)
					}
					_ = os.Chown(oldBackupDir, uid, gid)

					if meta.Pgrman.ArcLogPath != "" {
						_ = os.MkdirAll(meta.Pgrman.ArcLogPath, 0755)
						_ = os.Chown(meta.Pgrman.ArcLogPath, uid, gid)
					}

					pgrmanBin := filepath.Join(newBinDir, "pg_rman")
					if _, err := os.Stat(pgrmanBin); err != nil {
						pgrmanBin = getPgrmanBin(meta)
					}

					upgMeta := meta
					upgMeta.DataDir = newDataDir
					upgMeta.BinPath = filepath.Join(newBinDir, "postgres")

					initCmdStr := fmt.Sprintf("%s init -B %s -D %s", pgrmanBin, oldBackupDir, newDataDir)
					execCmdStr := utils.BuildInstanceCmd(upgMeta, initCmdStr)
					execCmd := exec.Command("su", "-s", "/bin/bash", "-", osUser, "-c", execCmdStr)
					out, err := execCmd.CombinedOutput()
					if err != nil {
						outStr := string(out)
						if !strings.Contains(strings.ToLower(outStr), "already initialized") {
							return fmt.Errorf("pg_rman init failed: %s", outStr)
						}
					}

					iniPath := filepath.Join(oldBackupDir, "pg_rman.ini")
					iniContent := fmt.Sprintf("SRVLOG_PATH='%s'\nARCLOG_PATH='%s'\nCOMPRESS_DATA=%s\nKEEP_ARCLOG_DAYS=%d\nKEEP_SRVLOG_DAYS=%d\nKEEP_DATA_DAYS=%d\n",
						meta.Pgrman.SrvLogPath, meta.Pgrman.ArcLogPath, meta.Pgrman.CompressData,
						meta.Pgrman.KeepArcLogDays, meta.Pgrman.KeepSrvLogDays, meta.Pgrman.KeepDataDays)

					if err := os.WriteFile(iniPath, []byte(iniContent), 0644); err != nil {
						return fmt.Errorf("failed to write pg_rman.ini: %w", err)
					}
					_ = os.Chown(iniPath, uid, gid)
				}
				return nil
			}); err != nil {
				return rollback(err)
			}
		}
	}

	// Step 5: Update systemd service file
	if err := executeStep(i18n.T("step_update_systemd"), func() error {
		var svcPath string
		var wantedBy string
		if osUser == "root" {
			svcPath = filepath.Join("/etc/systemd/system", serviceName)
			wantedBy = "multi-user.target"
		} else {
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
`, UpgConfig.InstanceName, newBinPath, meta.DataDir, wantedBy)

		if err := os.WriteFile(svcPath, []byte(svcContent), 0644); err != nil {
			return err
		}
		return os.Chown(svcPath, uid, gid)
	}); err != nil {
		return err
	}

	// Step 6: Update user environment (.pgrc)
	if err := executeStep(i18n.T("step_update_env"), func() error {
		pgrcPath := filepath.Join(u.HomeDir, ".pgrc")
		backupDir := filepath.Join(baseDir, fmt.Sprintf("backup_%s", UpgConfig.InstanceName))
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
		}

		if err := utils.UpdatePgrc(pgrcPath, envs); err != nil {
			return err
		}
		return os.Chown(pgrcPath, uid, gid)
	}); err != nil {
		return err
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
		return err
	}

	// Step 8: Update registry
	if err := executeStep(i18n.T("step_update_registry"), func() error {
		return config.SaveInstanceToRegistry(UpgConfig.InstanceName, osUser, meta.DataDir, newBinPath, meta.Port)
	}); err != nil {
		return err
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
