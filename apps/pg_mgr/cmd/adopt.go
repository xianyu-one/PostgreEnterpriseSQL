package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/process"
	"pg_mgr/internal/utils"
)

var (
	adoptDataDir string
	adoptOSUser  string
	adoptBinPath string
	adoptPort    string
	adoptName    string
)

var adoptCmd = &cobra.Command{
	Use:   "adopt",
	Short: i18n.T("adopt_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runAdopt() },
}

func init() {
	adoptCmd.Flags().StringVarP(&adoptDataDir, "data-dir", "d", "", "Data directory of the unstarted PostgreSQL instance to adopt")
	adoptCmd.Flags().StringVarP(&adoptOSUser, "os-user", "u", "postgres", "OS user who runs the database instance")
	adoptCmd.Flags().StringVarP(&adoptBinPath, "bin-path", "b", "", "Path to the postgres binary")
	adoptCmd.Flags().StringVarP(&adoptPort, "port", "p", "", "Port of the database instance")
	adoptCmd.Flags().StringVarP(&adoptName, "name", "i", "", "Instance name")

	RootCmd.AddCommand(adoptCmd)
}

func runAdopt() {
	if os.Geteuid() != 0 {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root")))
		os.Exit(1)
	}

	if adoptDataDir != "" {
		adoptUnstarted(adoptDataDir, adoptOSUser, adoptBinPath, adoptPort, adoptName)
		return
	}

	managedDirs := make(map[string]bool)
	for _, meta := range config.Global.Instances {
		managedDirs[filepath.Clean(meta.DataDir)] = true
	}

	var candidates []process.PgProcess
	runningProcs := process.FindPgProcesses()
	for _, proc := range runningProcs {
		if !managedDirs[filepath.Clean(proc.DataDir)] {
			candidates = append(candidates, proc)
		}
	}

	if len(candidates) == 0 {
		fmt.Println(text.FgHiYellow.Sprint(i18n.T("adopt_none")))
		if utils.PromptConfirm(i18n.T("prompt_adopt_unstarted")) {
			adoptUnstarted("", "", "", "", "")
		}
		return
	}

	fmt.Println(text.FgHiCyan.Sprint(i18n.T("adopt_found")))
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"ID", "PID", "OS User", "Data Directory", "Bin Path", "Port"})
	for i, c := range candidates {
		t.AppendRow(table.Row{i + 1, c.PID, c.OSUser, c.DataDir, c.BinPath, c.Port})
	}
	t.Render()

	idxStr := utils.PromptInput(i18n.T("prompt_adopt_idx"), "0")
	if strings.ToLower(idxStr) == "m" || strings.ToLower(idxStr) == "manual" {
		adoptUnstarted("", "", "", "", "")
		return
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 1 || idx > len(candidates) {
		if idx == 0 {
			fmt.Println(i18n.T("abort"))
		} else {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_invalid_id")))
		}
		return
	}

	target := candidates[idx-1]
	instName := utils.PromptInput(i18n.T("prompt_inst"), "legacy_"+target.Port)
	osUser := utils.PromptInput(i18n.T("prompt_os_user"), target.OSUser)

	u, err := user.Lookup(osUser)
	if err != nil {
		if utils.PromptConfirm(i18n.T("prompt_create_user", osUser)) {
			_ = utils.RunCmd("groupadd", osUser)
			if err := utils.RunCmd("useradd", "-g", osUser, "-m", osUser); err != nil {
				fmt.Println(i18n.T("create_user_failed", osUser, err))
				return
			}
			u, err = user.Lookup(osUser)
			if err != nil {
				fmt.Println(i18n.T("err_user_not_found", osUser))
				return
			}
			fmt.Println(i18n.T("create_user_success", osUser))
		} else {
			fmt.Println(i18n.T("err_user_not_found", osUser))
			return
		}
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

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

	// Detect old service files before killing the process
	oldServiceFiles := detectOldServiceFiles(target)

	executeStep(i18n.T("step_kill_old"), func() error {
		exec.Command("kill", "-INT", target.PID).Run()
		for i := 0; i < 15; i++ {
			if err := exec.Command("kill", "-0", target.PID).Run(); err != nil {
				return nil
			}
			time.Sleep(500 * time.Millisecond)
		}
		return fmt.Errorf("process did not stop gracefully")
	})

	executeStep(i18n.T("step_user"), func() error {
		if osUser != "root" {
			utils.RunCmd("loginctl", "enable-linger", osUser)
		}
		// Try to fix permissions gently
		filepath.Walk(target.DataDir, func(path string, info os.FileInfo, err error) error {
			if err == nil {
				os.Chown(path, uid, gid)
			}
			return nil
		})
		return nil
	})

	executeStep(i18n.T("step_pgconf"), func() error {
		confPath := filepath.Join(target.DataDir, "postgresql.conf")
		utils.ReplaceInFile(confPath, `(?m)^#?logging_collector\s*=.*`, "logging_collector = on")
		utils.ReplaceInFile(confPath, `(?m)^#?password_encryption\s*=.*`, "password_encryption = scram-sha-256")
		utils.ReplaceInFile(confPath, `(?m)^#?listen_addresses\s*=.*`, "listen_addresses = '0.0.0.0'")

		hbaPath := filepath.Join(target.DataDir, "pg_hba.conf")
		content, _ := os.ReadFile(hbaPath)
		if !strings.Contains(string(content), "0.0.0.0/0          scram-sha-256") {
			utils.AppendToFile(hbaPath, "\nhost    all             all             0.0.0.0/0          scram-sha-256\n")
		}
		return nil
	})

	serviceName := fmt.Sprintf("postgresql-%s.service", instName)
	executeStep(i18n.T("step_systemd"), func() error {
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
`, instName, target.BinPath, target.DataDir, wantedBy)

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

	// Add to Global Registry
	config.SaveInstanceToRegistry(instName, osUser, target.DataDir, target.BinPath, target.Port)

	pw.Stop()
	fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))

	if len(oldServiceFiles) > 0 {
		var pathsStr strings.Builder
		for _, file := range oldServiceFiles {
			pathsStr.WriteString(fmt.Sprintf("  - %s\n", file))
		}
		fmt.Print(i18n.T("warn_old_services", pathsStr.String()))
	}
}

func detectOldServiceFiles(target process.PgProcess) []string {
	var foundFiles []string

	// 1. Get candidate service names from cgroup
	cgroupPath := fmt.Sprintf("/proc/%s/cgroup", target.PID)
	content, err := os.ReadFile(cgroupPath)
	var candidateServices []string
	if err == nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			parts := strings.Split(line, ":")
			if len(parts) < 3 {
				continue
			}
			path := parts[2]
			pathParts := strings.Split(path, "/")
			for _, part := range pathParts {
				if strings.HasSuffix(part, ".service") {
					// Filter out generic systemd templates/scopes
					if strings.Contains(part, "user@") ||
						strings.Contains(part, "session-") ||
						strings.Contains(part, "user-runtime-dir@") ||
						strings.Contains(part, "init.scope") {
						continue
					}
					candidateServices = append(candidateServices, part)
				}
			}
		}
	}

	// 2. Define all search paths
	searchDirs := []string{
		"/etc/systemd/system",
		"/lib/systemd/system",
		"/usr/lib/systemd/system",
		"/run/systemd/system",
		"/etc/systemd/user",
		"/usr/lib/systemd/user",
	}

	if u, err := user.Lookup(target.OSUser); err == nil {
		searchDirs = append(searchDirs, filepath.Join(u.HomeDir, ".config/systemd/user"))
		searchDirs = append(searchDirs, filepath.Join(u.HomeDir, ".local/share/systemd/user"))
	}

	// Make unique and clean
	uniqueDirs := make(map[string]bool)
	var dirs []string
	for _, d := range searchDirs {
		clean := filepath.Clean(d)
		if !uniqueDirs[clean] {
			uniqueDirs[clean] = true
			dirs = append(dirs, clean)
		}
	}

	// Keep track of matched service files
	matchedFiles := make(map[string]bool)

	// Check if a service file is related to our target PgProcess.
	checkFile := func(path string) {
		realPath, err := filepath.EvalSymlinks(path)
		if err == nil {
			path = realPath
		}
		filename := filepath.Base(path)

		// Check by candidate name
		for _, cand := range candidateServices {
			if filename == cand {
				if !matchedFiles[path] {
					matchedFiles[path] = true
					foundFiles = append(foundFiles, path)
				}
				return
			}
			// Handle template instance like postgresql@14-main.service -> postgresql@.service
			if strings.Contains(cand, "@") {
				parts := strings.SplitN(cand, "@", 2)
				templateName := parts[0] + "@.service"
				if filename == templateName {
					if !matchedFiles[path] {
						matchedFiles[path] = true
						foundFiles = append(foundFiles, path)
					}
					return
				}
			}
		}

		// Also check file content for DataDir
		fileContent, err := os.ReadFile(path)
		if err == nil {
			if strings.Contains(string(fileContent), target.DataDir) {
				if !matchedFiles[path] {
					matchedFiles[path] = true
					foundFiles = append(foundFiles, path)
				}
			}
		}
	}

	// Scan directories
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".service") {
				continue
			}
			checkFile(filepath.Join(dir, entry.Name()))
		}
	}

	return foundFiles
}

func adoptUnstarted(dataDir, osUser, binPath, port, name string) {
	if dataDir == "" {
		dataDir = utils.PromptInput(i18n.T("prompt_data_dir"), "")
	}
	if dataDir == "" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_data_dir_required")))
		return
	}

	dataDirClean := filepath.Clean(dataDir)
	if _, err := os.Stat(dataDirClean); os.IsNotExist(err) {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_data_dir_not_found", dataDir)))
		return
	}

	// Check if already managed
	for _, meta := range config.Global.Instances {
		if filepath.Clean(meta.DataDir) == dataDirClean {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_already_managed", dataDir)))
			return
		}
	}

	if osUser == "" {
		osUser = utils.PromptInput(i18n.T("prompt_os_user"), "postgres")
	}
	u, err := user.Lookup(osUser)
	if err != nil {
		if utils.PromptConfirm(i18n.T("prompt_create_user", osUser)) {
			_ = utils.RunCmd("groupadd", osUser)
			if err := utils.RunCmd("useradd", "-g", osUser, "-m", osUser); err != nil {
				fmt.Println(text.FgHiRed.Sprint(i18n.T("create_user_failed", osUser, err)))
				return
			}
			u, err = user.Lookup(osUser)
			if err != nil {
				fmt.Println(text.FgHiRed.Sprint(i18n.T("err_user_not_found", osUser)))
				return
			}
			fmt.Println(text.FgHiGreen.Sprint(i18n.T("create_user_success", osUser)))
		} else {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("err_user_not_found", osUser)))
			return
		}
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// Try to auto-detect postgres version and default bin path if not provided
	detectedBinPath := ""
	pgVerBytes, err := os.ReadFile(filepath.Join(dataDirClean, "PG_VERSION"))
	if err == nil {
		majorStr := strings.TrimSpace(string(pgVerBytes))
		baseDir := config.Global.BaseDir
		installed, err := utils.GetInstalledVersions(baseDir)
		if err == nil {
			var matchingVersions []string
			for _, v := range installed {
				if strconv.Itoa(v.Major) == majorStr {
					matchingVersions = append(matchingVersions, filepath.Join(baseDir, strconv.Itoa(v.Major), strconv.Itoa(v.Minor), "bin", "postgres"))
				}
			}
			if len(matchingVersions) > 0 {
				detectedBinPath = matchingVersions[len(matchingVersions)-1]
			}
		}
	}

	if binPath == "" {
		binPath = utils.PromptInput(i18n.T("prompt_bin_path"), detectedBinPath)
	}
	if binPath == "" {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_bin_path_required")))
		return
	}
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_bin_path_not_found", binPath)))
		return
	}

	detectedPort := ""
	confPath := filepath.Join(dataDirClean, "postgresql.conf")
	if _, err := os.Stat(confPath); err == nil {
		detectedPort = utils.ExtractRegexFromFile(confPath, `(?m)^#?port\s*=\s*(\d+)`)
	}
	if detectedPort == "" {
		detectedPort = "5432"
	}

	if port == "" {
		port = utils.PromptInput(i18n.T("prompt_port"), detectedPort)
	}

	if name == "" {
		name = utils.PromptInput(i18n.T("prompt_inst"), "legacy_"+port)
	}

	if _, exists := config.Global.Instances[name]; exists {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_inst_exists", name)))
		return
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

	executeStep(i18n.T("step_user"), func() error {
		if osUser != "root" {
			utils.RunCmd("loginctl", "enable-linger", osUser)
		}
		filepath.Walk(dataDirClean, func(path string, info os.FileInfo, err error) error {
			if err == nil {
				os.Chown(path, uid, gid)
			}
			return nil
		})
		return nil
	})

	executeStep(i18n.T("step_pgconf"), func() error {
		utils.ReplaceInFile(confPath, `(?m)^#?logging_collector\s*=.*`, "logging_collector = on")
		utils.ReplaceInFile(confPath, `(?m)^#?password_encryption\s*=.*`, "password_encryption = scram-sha-256")
		utils.ReplaceInFile(confPath, `(?m)^#?listen_addresses\s*=.*`, "listen_addresses = '0.0.0.0'")
		utils.ReplaceInFile(confPath, `(?m)^#?port\s*=.*`, fmt.Sprintf("port = %s", port))

		hbaPath := filepath.Join(dataDirClean, "pg_hba.conf")
		content, _ := os.ReadFile(hbaPath)
		if !strings.Contains(string(content), "0.0.0.0/0          scram-sha-256") {
			utils.AppendToFile(hbaPath, "\nhost    all             all             0.0.0.0/0          scram-sha-256\n")
		}
		return nil
	})

	serviceName := fmt.Sprintf("postgresql-%s.service", name)
	executeStep(i18n.T("step_systemd"), func() error {
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
`, name, binPath, dataDirClean, wantedBy)

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

	// Add to Global Registry
	config.SaveInstanceToRegistry(name, osUser, dataDirClean, binPath, port)

	pw.Stop()
	fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
}
