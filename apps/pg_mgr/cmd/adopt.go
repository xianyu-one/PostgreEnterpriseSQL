package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
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
	RunE:  func(cmd *cobra.Command, args []string) error { return runAdopt() },
}

func init() {
	adoptCmd.Flags().StringVarP(&adoptDataDir, "data-dir", "d", "", "Data directory of the unstarted PostgreSQL instance to adopt")
	adoptCmd.Flags().StringVarP(&adoptOSUser, "os-user", "u", "postgres", "OS user who runs the database instance")
	adoptCmd.Flags().StringVarP(&adoptBinPath, "bin-path", "b", "", "Path to the postgres binary")
	adoptCmd.Flags().StringVarP(&adoptPort, "port", "p", "", "Port of the database instance")
	adoptCmd.Flags().StringVarP(&adoptName, "name", "i", "", "Instance name")

	InstanceCmd.AddCommand(adoptCmd)
	RootCmd.AddCommand(adoptCmd)
}

func runAdopt() (runErr error) {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	if UI.NonInteractive && adoptDataDir == "" {
		return interaction.MissingFlags("--data-dir")
	}
	if UI.NonInteractive && adoptName == "" {
		return interaction.MissingFlags("--name")
	}

	if adoptDataDir != "" {
		return adoptUnstarted(adoptDataDir, adoptOSUser, adoptBinPath, adoptPort, adoptName)
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
			return adoptUnstarted("", "", "", "", "")
		}
		return nil
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
		return adoptUnstarted("", "", "", "", "")
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

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	operation := interaction.NewOperation(os.Stderr, mode)
	executeStep := func(msg string, action func() error) error {
		return operation.Run(msg, action)
	}

	// Detect old service files before killing the process
	oldServiceFiles := detectOldServiceFiles(target)

	if err := executeStep(i18n.T("step_kill_old"), func() error {
		pidInt, _ := strconv.Atoi(target.PID)
		syscall.Kill(pidInt, syscall.SIGINT)
		for i := 0; i < 15; i++ {
			if err := syscall.Kill(pidInt, 0); err != nil {
				return nil
			}
			time.Sleep(500 * time.Millisecond)
		}
		return fmt.Errorf("process did not stop gracefully")
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_user"), func() error {
		if osUser != "root" {
			if err := utils.RunCmd("loginctl", "enable-linger", osUser); err != nil {
				return err
			}
		}
		// Ensure pkg directory permissions for target.BinPath
		if binDir := filepath.Dir(filepath.Dir(target.BinPath)); binDir != "" && binDir != "." && binDir != "/" {
			if err := utils.EnsurePkgPermissions(binDir); err != nil {
				return err
			}
		}
		return prepareAdoptDataDir(target.DataDir, uid, gid)
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_pgconf"), func() error {
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
	}); err != nil {
		return err
	}

	serviceName := fmt.Sprintf("postgresql-%s.service", instName)
	if err := executeStep(i18n.T("step_systemd"), func() error {
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
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_start"), func() error {
		if osUser == "root" {
			if err := utils.RunCmd("systemctl", "daemon-reload"); err != nil {
				return err
			}
			if err := utils.RunCmd("systemctl", "enable", serviceName); err != nil {
				return err
			}
			return utils.RunCmd("systemctl", "start", serviceName)
		}
		if err := utils.RunAsUser(osUser, "systemctl --user daemon-reload"); err != nil {
			return err
		}
		if err := utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user enable %s", serviceName)); err != nil {
			return err
		}
		return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user start %s", serviceName))
	}); err != nil {
		return err
	}

	// Add to Global Registry
	if err := config.SaveInstanceToRegistry(instName, osUser, target.DataDir, target.BinPath, target.Port); err != nil {
		return err
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": instName, "status": "adopted", "operation": operation.Result()})
	}
	if !UI.Quiet {
		fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
	}

	if len(oldServiceFiles) > 0 {
		var pathsStr strings.Builder
		for _, file := range oldServiceFiles {
			pathsStr.WriteString(fmt.Sprintf("  - %s\n", file))
		}
		fmt.Print(i18n.T("warn_old_services", pathsStr.String()))
	}
	return nil
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

func adoptUnstarted(dataDir, osUser, binPath, port, name string) (runErr error) {
	if dataDir == "" {
		if UI.NonInteractive {
			return interaction.MissingFlags("--data-dir")
		}
		dataDir = utils.PromptPath(i18n.T("prompt_data_dir"), "")
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

	defaultUser := osUser
	if defaultUser == "" || defaultUser == "postgres" {
		if detected := utils.DetectDirOwner(dataDirClean); detected != "" {
			defaultUser = detected
		} else {
			defaultUser = "postgres"
		}
	}
	if UI.NonInteractive {
		osUser = defaultUser
	} else {
		osUser = utils.PromptInput(i18n.T("prompt_os_user"), defaultUser)
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

	if binPath == "" {
		compatible, err := compatibleInstalledVersions(dataDirClean, config.Global.BaseDir)
		if err != nil {
			fmt.Println(text.FgHiRed.Sprint(err))
			return
		}
		selected := compatible[len(compatible)-1]
		if !UI.NonInteractive {
			selected, err = promptInstalledVersion(i18n.T("prompt_select_version"), compatible, len(compatible)-1)
			if err != nil {
				return err
			}
		}
		binPath = postgresBinPath(config.Global.BaseDir, selected)
	}
	if info, err := os.Stat(binPath); err != nil || info.IsDir() {
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
		if UI.NonInteractive {
			port = detectedPort
		} else {
			port = utils.PromptInput(i18n.T("prompt_port"), detectedPort)
		}
	}

	if name == "" {
		if UI.NonInteractive {
			return interaction.MissingFlags("--name")
		}
		name = utils.PromptInput(i18n.T("prompt_inst"), "legacy_"+port)
	}

	if _, exists := config.Global.Instances[name]; exists {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("err_inst_exists", name)))
		return
	}

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	operation := interaction.NewOperation(os.Stderr, mode)
	executeStep := func(msg string, action func() error) error {
		return operation.Run(msg, action)
	}

	if err := executeStep(i18n.T("step_user"), func() error {
		if osUser != "root" {
			if err := utils.RunCmd("loginctl", "enable-linger", osUser); err != nil {
				return err
			}
		}
		if binDir := filepath.Dir(filepath.Dir(binPath)); binDir != "" && binDir != "." && binDir != "/" {
			if err := utils.EnsurePkgPermissions(binDir); err != nil {
				return err
			}
		}
		return prepareAdoptDataDir(dataDirClean, uid, gid)
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_pgconf"), func() error {
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
	}); err != nil {
		return err
	}

	serviceName := fmt.Sprintf("postgresql-%s.service", name)
	if err := executeStep(i18n.T("step_systemd"), func() error {
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
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_start"), func() error {
		if osUser == "root" {
			if err := utils.RunCmd("systemctl", "daemon-reload"); err != nil {
				return err
			}
			if err := utils.RunCmd("systemctl", "enable", serviceName); err != nil {
				return err
			}
			return utils.RunCmd("systemctl", "start", serviceName)
		}
		if err := utils.RunAsUser(osUser, "systemctl --user daemon-reload"); err != nil {
			return err
		}
		if err := utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user enable %s", serviceName)); err != nil {
			return err
		}
		return utils.RunAsUser(osUser, fmt.Sprintf("systemctl --user start %s", serviceName))
	}); err != nil {
		return err
	}

	// Add to Global Registry
	if err := config.SaveInstanceToRegistry(name, osUser, dataDirClean, binPath, port); err != nil {
		return err
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"instance": name, "status": "adopted", "operation": operation.Result()})
	}
	if !UI.Quiet {
		fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
	}
	return nil
}

func postgresBinPath(baseDir string, version utils.PGVersion) string {
	return filepath.Join(baseDir, strconv.Itoa(version.Major), strconv.Itoa(version.Minor), "bin", "postgres")
}

func compatibleInstalledVersions(dataDir, baseDir string) ([]utils.PGVersion, error) {
	versionBytes, err := os.ReadFile(filepath.Join(dataDir, "PG_VERSION"))
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("err_read_pg_version", err))
	}
	major, err := strconv.Atoi(strings.TrimSpace(string(versionBytes)))
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("err_invalid_pg_version", strings.TrimSpace(string(versionBytes))))
	}
	installed, err := utils.GetInstalledVersions(baseDir)
	if err != nil {
		return nil, err
	}
	compatible := make([]utils.PGVersion, 0, len(installed))
	for _, version := range installed {
		if version.Major == major {
			compatible = append(compatible, version)
		}
	}
	if len(compatible) == 0 {
		return nil, fmt.Errorf("%s", i18n.T("err_no_compatible_version", major))
	}
	return compatible, nil
}

func prepareAdoptDataDir(dataDir string, uid, gid int) error {
	if err := os.Chmod(dataDir, 0700); err != nil {
		return fmt.Errorf("chmod %s to 0700: %w", dataDir, err)
	}
	return filepath.Walk(dataDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
		return nil
	})
}
