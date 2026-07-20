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
	"pg_mgr/internal/i18n"
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
	Run:   func(cmd *cobra.Command, args []string) { runDaemonInstall() },
}

var daemonUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: i18n.T("daemon_uninstall_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runDaemonUninstall() },
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: i18n.T("daemon_start_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runDaemonStart() },
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: i18n.T("daemon_stop_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runDaemonStop() },
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: i18n.T("daemon_status_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runDaemonStatus() },
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: i18n.T("daemon_restart_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runDaemonRestart() },
}

var daemonReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: i18n.T("daemon_reload_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runDaemonReload() },
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

var ensureRootFunc = func() {
	if os.Geteuid() != 0 {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root")))
		os.Exit(1)
	}
}

func ensureRoot() {
	ensureRootFunc()
}

func runDaemonInstall() {
	ensureRoot()
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println(i18n.T("daemon_failed", err))
		os.Exit(1)
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
		fmt.Println(i18n.T("daemon_failed", err))
		os.Exit(1)
	}

	if err := utils.RunCmd("systemctl", "daemon-reload"); err != nil {
		fmt.Println(i18n.T("daemon_failed", err))
		os.Exit(1)
	}
	if err := utils.RunCmd("systemctl", "enable", "pg_mgr.service"); err != nil {
		fmt.Println(i18n.T("daemon_failed", err))
		os.Exit(1)
	}

	fmt.Println(text.FgHiGreen.Sprint(i18n.T("daemon_installed")))
}

func runDaemonUninstall() {
	ensureRoot()
	_ = utils.RunCmd("systemctl", "stop", "pg_mgr.service")
	_ = utils.RunCmd("systemctl", "disable", "pg_mgr.service")
	_ = os.Remove("/etc/systemd/system/pg_mgr.service")
	_ = utils.RunCmd("systemctl", "daemon-reload")
	fmt.Println(text.FgHiGreen.Sprint(i18n.T("daemon_uninstalled")))
}

func runDaemonStart() {
	ensureRoot()
	if err := utils.RunCmd("systemctl", "start", "pg_mgr.service"); err != nil {
		fmt.Println(i18n.T("daemon_failed", err))
		os.Exit(1)
	}
	fmt.Println(text.FgHiGreen.Sprint(i18n.T("daemon_started")))
}

func runDaemonStop() {
	ensureRoot()
	if err := utils.RunCmd("systemctl", "stop", "pg_mgr.service"); err != nil {
		fmt.Println(i18n.T("daemon_failed", err))
		os.Exit(1)
	}
	fmt.Println(text.FgHiGreen.Sprint(i18n.T("daemon_stopped")))
}

func runDaemonRestart() {
	ensureRoot()
	if err := utils.RunCmd("systemctl", "restart", "pg_mgr.service"); err != nil {
		fmt.Println(i18n.T("daemon_failed", err))
		os.Exit(1)
	}
	fmt.Println(text.FgHiGreen.Sprint(i18n.T("daemon_restarted")))
}

func runDaemonReload() {
	ensureRoot()
	if err := utils.RunCmd("systemctl", "reload", "pg_mgr.service"); err != nil {
		fmt.Println(i18n.T("daemon_failed", err))
		os.Exit(1)
	}
	fmt.Println(text.FgHiGreen.Sprint(i18n.T("daemon_reloaded")))
}

func runDaemonStatus() {
	ensureRoot()
	cmd := exec.Command("systemctl", "status", "pg_mgr.service")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
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

	pgrmanBin := getPgrmanBin(instance)
	cmdStr := fmt.Sprintf("%s backup -p %s -D %s --backup-mode=%s --with-serverlog -B %s && %s validate -B %s",
		pgrmanBin, instance.Port, instance.DataDir, mode, bk.BackupDir, pgrmanBin, bk.BackupDir)
	logger.Info("Executing backup command: %s as user %s", cmdStr, instance.User)

	cmd := exec.Command("su", "-s", "/bin/bash", "-", instance.User, "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command execution failed: %v, output: %s", err, string(out))
	}
	logger.Info("Backup output:\n%s", string(out))
	return nil
}
