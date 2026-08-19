package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/database"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/logger"
	"pg_mgr/internal/utils"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: i18n.T("daemon_desc"),
}

var daemonInstallCmd = &cobra.Command{
	Use:   "install",
	Short: i18n.T("daemon_install_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDaemonInstall() },
}

var daemonUninstallCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"uninstall"},
	Short:   i18n.T("daemon_uninstall_desc"),
	RunE:    func(cmd *cobra.Command, args []string) error { return runDaemonUninstall() },
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: i18n.T("daemon_start_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDaemonStart() },
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: i18n.T("daemon_stop_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDaemonStop() },
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: i18n.T("daemon_status_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDaemonStatus() },
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: i18n.T("daemon_restart_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDaemonRestart() },
}

var daemonReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: i18n.T("daemon_reload_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDaemonReload() },
}

var daemonRunCmd = &cobra.Command{
	Use:   "run",
	Short: i18n.T("daemon_run_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runDaemonRun() },
}

func init() {
	daemonCmd.AddCommand(daemonInstallCmd, daemonUninstallCmd, daemonStartCmd, daemonStopCmd, daemonRestartCmd, daemonReloadCmd, daemonStatusCmd, daemonRunCmd)
	RootCmd.AddCommand(daemonCmd)
}

var ensureRootErr error

var ensureRootFunc = func() { ensureRootErr = utils.CheckRoot() }

func ensureRoot() error {
	ensureRootErr = nil
	ensureRootFunc()
	return ensureRootErr
}

func runDaemonInstall() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	servicePath := "/etc/systemd/system/pg_mgr.service"
	content := fmt.Sprintf(`[Unit]
Description=pg_mgr System Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s daemon run
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
User=root

[Install]
WantedBy=multi-user.target
`, exePath)

	err = os.WriteFile(servicePath, []byte(content), 0644)
	if err != nil {
		return err
	}

	if err := utils.RunCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := utils.RunCmd("systemctl", "enable", "pg_mgr.service"); err != nil {
		return err
	}

	return renderDaemonSuccess("installed", i18n.T("daemon_installed"))
}

func runDaemonUninstall() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	if UI.NonInteractive && !UI.Yes {
		return interaction.MissingFlags("--yes")
	}
	if err := utils.RunCmd("systemctl", "stop", "pg_mgr.service"); err != nil {
		return err
	}
	if err := utils.RunCmd("systemctl", "disable", "pg_mgr.service"); err != nil {
		return err
	}
	if err := os.Remove("/etc/systemd/system/pg_mgr.service"); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := utils.RunCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}
	return renderDaemonSuccess("removed", i18n.T("daemon_uninstalled"))
}

func runDaemonStart() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	if err := utils.RunCmd("systemctl", "start", "pg_mgr.service"); err != nil {
		return err
	}
	return renderDaemonSuccess("started", i18n.T("daemon_started"))
}

func runDaemonStop() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	if err := utils.RunCmd("systemctl", "stop", "pg_mgr.service"); err != nil {
		return err
	}
	return renderDaemonSuccess("stopped", i18n.T("daemon_stopped"))
}

func runDaemonRestart() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	if err := utils.RunCmd("systemctl", "restart", "pg_mgr.service"); err != nil {
		return err
	}
	return renderDaemonSuccess("restarted", i18n.T("daemon_restarted"))
}

func runDaemonReload() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	if err := utils.RunCmd("systemctl", "reload", "pg_mgr.service"); err != nil {
		return err
	}
	return renderDaemonSuccess("reloaded", i18n.T("daemon_reloaded"))
}

func runDaemonStatus() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	cmd := exec.Command("systemctl", "status", "pg_mgr.service")
	if UI.Output == string(interaction.OutputJSON) {
		output, err := cmd.CombinedOutput()
		if err != nil {
			return err
		}
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"service": "pg_mgr.service", "status": string(output)})
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func renderDaemonSuccess(status, message string) error {
	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"service": "pg_mgr.service", "status": status})
	}
	if !UI.Quiet {
		fmt.Println(text.FgHiGreen.Sprint(message))
	}
	return nil
}

func matchCron(cronExpr string, now time.Time) bool {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	sched, err := parser.Parse(cronExpr)
	if err != nil {
		return false
	}
	currentMin := now.Truncate(time.Minute)
	prevSecond := currentMin.Add(-1 * time.Second)
	nextRun := sched.Next(prevSecond)
	return nextRun.Equal(currentMin)
}

func runDaemonRun() {
	ensureRoot()
	logger.InitLogger()
	defer logger.Close()

	logger.Info("pg_mgr daemon starting...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)
	go func() {
		for range sigChan {
			logger.Info("Received SIGHUP, reloading configuration...")
			config.InitConfig()
		}
	}()

	lastFullRunMinute := make(map[string]string)
	lastIncrRunMinute := make(map[string]string)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		// Reload configuration dynamically
		config.InitConfig()

		now := time.Now()
		currentMinStr := now.Format("2006-01-02 15:04")

		for name, meta := range config.Global.Instances {
			if meta.Pgrman == nil {
				continue
			}
			bk := meta.Pgrman
			if bk.Tool != "pgrman" {
				continue
			}
			if !isBackupScheduleEnabled(bk) {
				continue
			}

			fullCron := getFullCronExpr(bk)
			incrCron := getIncrCronExpr(bk)

			// Check Full backup schedule
			if matchCron(fullCron, now) {
				if lastFullRunMinute[name] != currentMinStr {
					lastFullRunMinute[name] = currentMinStr
					logger.Info("Time matches full backup schedule (%s) for instance '%s'. Launching backup...", fullCron, name)
					go func(instName string, instance config.InstanceMeta) {
						err := runPgRmanBackup(instName, instance, "full")
						if err != nil {
							logger.Error("Full backup for instance '%s' failed: %v", instName, err)
						} else {
							logger.Info("Full backup for instance '%s' completed successfully", instName)
						}
					}(name, meta)
				}
			}

			// Check Incremental backup schedule
			if matchCron(incrCron, now) {
				// Don't run incremental backup if we already launched full backup at this exact minute
				if lastIncrRunMinute[name] != currentMinStr && lastFullRunMinute[name] != currentMinStr {
					lastIncrRunMinute[name] = currentMinStr
					logger.Info("Time matches incremental backup schedule (%s) for instance '%s'. Launching backup...", incrCron, name)
					go func(instName string, instance config.InstanceMeta) {
						err := runPgRmanBackup(instName, instance, "incremental")
						if err != nil {
							logger.Error("Incremental backup for instance '%s' failed: %v", instName, err)
						} else {
							logger.Info("Incremental backup for instance '%s' completed successfully", instName)
						}
					}(name, meta)
				}
			}
		}

		select {
		case <-ticker.C:
		}
	}
}

func runPgRmanBackup(name string, instance config.InstanceMeta, mode string) error {
	bk := instance.Pgrman
	if bk == nil {
		return fmt.Errorf("no pgrman backup configuration for instance %s", name)
	}

	connection, err := database.Resolve(name, instance, false)
	if err != nil {
		return err
	}
	cmdStr := buildPgRmanBackupCommand(instance, mode, connection)
	fullCmdStr := utils.BuildInstanceCmd(instance, cmdStr)
	logger.Info("Executing backup command: %s as user %s", fullCmdStr, instance.User)

	cmd := exec.Command("su", "-s", "/bin/bash", "-", instance.User, "-c", fullCmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command execution failed: %v, output: %s", err, string(out))
	}
	logger.Info("Backup output:\n%s", string(out))
	return nil
}
