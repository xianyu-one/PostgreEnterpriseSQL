package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// 国际化支持
var translations = map[string]map[string]string{
	"en": {
		"desc_root":       "PG WAL Cleaner - A portable tool to clean PostgreSQL WAL archives",
		"desc_clean":      "Run the cleanup process once immediately",
		"desc_daemon":     "Run the cleanup process periodically in the background",
		"desc_install":    "Install the cleaner as a systemd service or crontab job",
		"flag_dir":        "Specify the WAL archive directory (absolute path recommended)",
		"flag_duration":   "Retention duration (e.g., 7d, 24h). Files older than this will be deleted",
		"flag_size":       "Maximum total size to keep (e.g., 10G, 500M)",
		"flag_interval":   "Interval for daemon/crontab (e.g., 30m, 1h)",
		"flag_lang":       "Language (en, zh-CN, zh-TW)",
		"flag_method":     "Installation method: systemd or crontab",
		"err_dir_req":     "Error: Archive directory (--dir) is required",
		"err_keep_req":    "Error: Must specify either --keep-duration or --keep-size",
		"msg_start":       "Starting WAL cleanup in directory: %s",
		"msg_deleted":     "Deleted WAL file: %s (Size: %s, ModTime: %s)",
		"msg_kept_size":   "Current retained size: %s / %s",
		"msg_finish":      "Cleanup finished. Total deleted: %d files, freed: %s",
		"msg_daemon_loop": "Sleeping for %s until next cleanup...",
		"err_parse_size":  "Invalid size format: %s",
		"err_parse_dur":   "Invalid duration format: %s",
		"msg_inst_sysd":   "Successfully installed systemd service: %s",
		"msg_inst_cron":   "Successfully installed crontab job",
		"msg_linger":      "\n[IMPORTANT] Installed as a user systemd service.\nTo ensure it starts on boot even when not logged in, please run:\n\n    sudo loginctl enable-linger %s\n",
	},
	"zh-CN": {
		"desc_root":       "PG WAL Cleaner - 轻量级 PostgreSQL WAL 归档清理工具",
		"desc_clean":      "立即执行一次清理任务",
		"desc_daemon":     "在后台循环执行清理任务",
		"desc_install":    "将清理工具一键安装至 systemd 或 crontab",
		"flag_dir":        "指定 WAL 归档目录（建议使用绝对路径）",
		"flag_duration":   "保留时间（如 7d, 24h）。超过此时间的旧文件将被删除",
		"flag_size":       "最大保留总大小（如 10G, 500M）",
		"flag_interval":   "后台/定时任务的执行间隔（默认 30m）",
		"flag_lang":       "语言 (en, zh-CN, zh-TW)",
		"flag_method":     "安装方式：systemd 或 crontab",
		"err_dir_req":     "错误：必须指定归档目录 (--dir)",
		"err_keep_req":    "错误：必须指定保留时间 (--keep-duration) 或保留大小 (--keep-size)",
		"msg_start":       "开始清理 WAL 归档目录: %s",
		"msg_deleted":     "已删除 WAL 文件: %s (大小: %s, 修改时间: %s)",
		"msg_kept_size":   "当前已保留大小: %s / %s",
		"msg_finish":      "清理完成。共删除 %d 个文件，释放空间 %s",
		"msg_daemon_loop": "休眠 %s 后进行下一次清理...",
		"err_parse_size":  "无效的大小格式: %s",
		"err_parse_dur":   "无效的时间格式: %s",
		"msg_inst_sysd":   "成功安装 systemd 服务: %s",
		"msg_inst_cron":   "成功安装 crontab 定时任务",
		"msg_linger":      "\n[重要提示] 已作为普通用户的 systemd 服务安装。\n为了确保该服务在系统重启后能够开机自启，请务必执行以下命令：\n\n    sudo loginctl enable-linger %s\n",
	},
	"zh-TW": {
		"desc_root":       "PG WAL Cleaner - 輕量級 PostgreSQL WAL 歸檔清理工具",
		"desc_clean":      "立即執行一次清理任務",
		"desc_daemon":     "在背景循環執行清理任務",
		"desc_install":    "將清理工具一鍵安裝至 systemd 或 crontab",
		"flag_dir":        "指定 WAL 歸檔目錄（建議使用絕對路徑）",
		"flag_duration":   "保留時間（如 7d, 24h）。超過此時間的舊檔案將被刪除",
		"flag_size":       "最大保留總大小（如 10G, 500M）",
		"flag_interval":   "背景/定時任務的執行間隔（預設 30m）",
		"flag_lang":       "語言 (en, zh-CN, zh-TW)",
		"flag_method":     "安裝方式：systemd 或 crontab",
		"err_dir_req":     "錯誤：必須指定歸檔目錄 (--dir)",
		"err_keep_req":    "錯誤：必須指定保留時間 (--keep-duration) 或保留大小 (--keep-size)",
		"msg_start":       "開始清理 WAL 歸檔目錄: %s",
		"msg_deleted":     "已刪除 WAL 檔案: %s (大小: %s, 修改時間: %s)",
		"msg_kept_size":   "目前已保留大小: %s / %s",
		"msg_finish":      "清理完成。共刪除 %d 個檔案，釋放空間 %s",
		"msg_daemon_loop": "休眠 %s 後進行下一次清理...",
		"err_parse_size":  "無效的大小格式: %s",
		"err_parse_dur":   "無效的時間格式: %s",
		"msg_inst_sysd":   "成功安裝 systemd 服務: %s",
		"msg_inst_cron":   "成功安裝 crontab 定時任務",
		"msg_linger":      "\n[重要提示] 已作為一般使用者的 systemd 服務安裝。\n為了確保該服務在系統重啟後能夠開機自啟，請務必執行以下命令：\n\n    sudo loginctl enable-linger %s\n",
	},
}

var currentLang = "en"

// T 获取对应语言的文本
func T(key string, args ...interface{}) string {
	dict, ok := translations[currentLang]
	if !ok {
		dict = translations["en"]
	}
	text, ok := dict[key]
	if !ok {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(text, args...)
	}
	return text
}

// 侦测系统默认语言
func detectLanguage() {
	langEnv := os.Getenv("LANG")
	if strings.Contains(langEnv, "zh_CN") {
		currentLang = "zh-CN"
	} else if strings.Contains(langEnv, "zh_TW") || strings.Contains(langEnv, "zh_HK") {
		currentLang = "zh-TW"
	} else {
		currentLang = "en"
	}
}

// 命令行标志变量
var (
	optDir      string
	optDuration string
	optSize     string
	optInterval string
	optLang     string
	optMethod   string
)

// WAL 文件名正则表达式验证 (标准WAL由24位大写十六进制组成，可能带有压缩后缀)
var walRegex = regexp.MustCompile(`^[0-9A-F]{24}(\.[a-zA-Z0-9]+)?$`)

// 将容量字符串 (如 "10G", "500M") 解析为字节数
func parseSize(sizeStr string) (int64, error) {
	if sizeStr == "" {
		return 0, nil
	}
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))
	multiplier := int64(1)
	if strings.HasSuffix(sizeStr, "G") || strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimRight(sizeStr, "GB")
		sizeStr = strings.TrimRight(sizeStr, "G")
	} else if strings.HasSuffix(sizeStr, "M") || strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimRight(sizeStr, "MB")
		sizeStr = strings.TrimRight(sizeStr, "M")
	} else if strings.HasSuffix(sizeStr, "K") || strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimRight(sizeStr, "KB")
		sizeStr = strings.TrimRight(sizeStr, "K")
	}

	val, err := strconv.ParseFloat(sizeStr, 64)
	if err != nil {
		return 0, fmt.Errorf(T("err_parse_size", sizeStr))
	}
	return int64(val * float64(multiplier)), nil
}

// 格式化字节数
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// 解析持续时间，支持 d 代表天数
func parseDuration(durStr string) (time.Duration, error) {
	if durStr == "" {
		return 0, nil
	}
	durStr = strings.ToLower(strings.TrimSpace(durStr))
	if strings.HasSuffix(durStr, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(durStr, "d"), 64)
		if err != nil {
			return 0, fmt.Errorf(T("err_parse_dur", durStr))
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	}
	return time.ParseDuration(durStr)
}

type fileInfo struct {
	path    string
	size    int64
	modTime time.Time
}

// 执行清理逻辑核心
func performCleanup() error {
	if optDir == "" {
		return fmt.Errorf(T("err_dir_req"))
	}

	sizeLimit, err := parseSize(optSize)
	if err != nil {
		return err
	}

	timeLimit, err := parseDuration(optDuration)
	if err != nil {
		return err
	}

	if sizeLimit == 0 && timeLimit == 0 {
		return fmt.Errorf(T("err_keep_req"))
	}

	fmt.Println(T("msg_start", optDir))

	var files []fileInfo
	err = filepath.WalkDir(optDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != optDir {
			return fs.SkipDir // 不递归子目录
		}
		if !d.IsDir() && walRegex.MatchString(d.Name()) {
			info, err := d.Info()
			if err == nil {
				files = append(files, fileInfo{
					path:    path,
					size:    info.Size(),
					modTime: info.ModTime(),
				})
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to scan directory: %v", err)
	}

	// 按照修改时间降序排序（最新的排前面）
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	var accumulatedSize int64 = 0
	var deletedCount int = 0
	var freedSpace int64 = 0

	for _, f := range files {
		shouldDelete := false

		// 检查大小限制 (满足最新的前提下超出保留大小限制的清理掉)
		if sizeLimit > 0 && (accumulatedSize+f.size) > sizeLimit {
			shouldDelete = true
		}

		// 检查时间限制 (超出时间的旧文件直接清理掉)
		if timeLimit > 0 && time.Since(f.modTime) > timeLimit {
			shouldDelete = true
		}

		if shouldDelete {
			if err := os.Remove(f.path); err == nil {
				fmt.Println(T("msg_deleted", filepath.Base(f.path), formatBytes(f.size), f.modTime.Format(time.RFC3339)))
				deletedCount++
				freedSpace += f.size
			} else {
				fmt.Printf("Failed to delete %s: %v\n", f.path, err)
			}
		} else {
			accumulatedSize += f.size
		}
	}

	if sizeLimit > 0 {
		fmt.Println(T("msg_kept_size", formatBytes(accumulatedSize), formatBytes(sizeLimit)))
	}
	fmt.Println(T("msg_finish", deletedCount, formatBytes(freedSpace)))

	return nil
}

func startDaemon() {
	interval, err := time.ParseDuration(optInterval)
	if err != nil {
		fmt.Println(T("err_parse_dur", optInterval))
		os.Exit(1)
	}

	for {
		err := performCleanup()
		if err != nil {
			fmt.Printf("Cleanup error: %v\n", err)
		}
		fmt.Println(T("msg_daemon_loop", interval.String()))
		time.Sleep(interval)
	}
}

func installService() error {
	absDir, err := filepath.Abs(optDir)
	if err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	if optMethod == "systemd" {
		return installSystemd(exePath, absDir)
	} else if optMethod == "crontab" {
		return installCrontab(exePath, absDir)
	}
	return fmt.Errorf("unknown installation method: %s", optMethod)
}

func installSystemd(exePath, absDir string) error {
	currentUser, err := user.Current()
	if err != nil {
		return err
	}
	isRoot := currentUser.Uid == "0"

	serviceContent := fmt.Sprintf(`[Unit]
Description=PG WAL Cleaner Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s daemon --dir %s --keep-duration "%s" --keep-size "%s" --interval %s
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`, exePath, absDir, optDuration, optSize, optInterval)

	var servicePath string
	if isRoot {
		servicePath = "/etc/systemd/system/pg_wal_cleaner.service"
	} else {
		configDir := filepath.Join(currentUser.HomeDir, ".config", "systemd", "user")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return err
		}
		servicePath = filepath.Join(configDir, "pg_wal_cleaner.service")
	}

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return err
	}

	// 重新加载并启用 Systemd
	var cmdReload, cmdEnable *exec.Cmd
	if isRoot {
		cmdReload = exec.Command("systemctl", "daemon-reload")
		cmdEnable = exec.Command("systemctl", "enable", "--now", "pg_wal_cleaner.service")
	} else {
		cmdReload = exec.Command("systemctl", "--user", "daemon-reload")
		cmdEnable = exec.Command("systemctl", "--user", "enable", "--now", "pg_wal_cleaner.service")
	}

	if err := cmdReload.Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %v", err)
	}
	if err := cmdEnable.Run(); err != nil {
		return fmt.Errorf("failed to enable systemd service: %v", err)
	}

	fmt.Println(T("msg_inst_sysd", servicePath))
	if !isRoot {
		fmt.Printf(T("msg_linger"), currentUser.Username)
	}

	return nil
}

func installCrontab(exePath, absDir string) error {
	intervalDur, err := time.ParseDuration(optInterval)
	if err != nil {
		return err
	}
	mins := int(intervalDur.Minutes())
	if mins < 1 {
		mins = 1
	}

	cronSpec := ""
	if mins < 60 {
		cronSpec = fmt.Sprintf("*/%d * * * *", mins)
	} else {
		hours := mins / 60
		cronSpec = fmt.Sprintf("0 */%d * * *", hours)
	}

	cmdStr := fmt.Sprintf("%s clean --dir %s --keep-duration \"%s\" --keep-size \"%s\" >> /tmp/pg_wal_cleaner.log 2>&1",
		exePath, absDir, optDuration, optSize)

	newJob := fmt.Sprintf("%s %s\n", cronSpec, cmdStr)

	// 获取当前用户的 crontab 状态
	var out bytes.Buffer
	cmdGet := exec.Command("crontab", "-l")
	cmdGet.Stdout = &out
	_ = cmdGet.Run() // 如果没有之前的任务会返回错误，这里忽略

	currentCron := out.String()
	if strings.Contains(currentCron, "pg_wal_cleaner clean") {
		return fmt.Errorf("crontab already contains a pg_wal_cleaner job")
	}

	newCron := currentCron
	if len(newCron) > 0 && !strings.HasSuffix(newCron, "\n") {
		newCron += "\n"
	}
	newCron += newJob

	// 写入新的 crontab
	cmdSet := exec.Command("crontab", "-")
	cmdSet.Stdin = strings.NewReader(newCron)
	if err := cmdSet.Run(); err != nil {
		return fmt.Errorf("failed to install crontab: %v", err)
	}

	fmt.Println(T("msg_inst_cron"))
	return nil
}

var Version = "dev"

func main() {
	detectLanguage()

	var rootCmd = &cobra.Command{
		Use:     "pg_wal_cleaner",
		Short:   T("desc_root"),
		Version: Version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if optLang != "" {
				currentLang = optLang
			}
		},
	}

	rootCmd.PersistentFlags().StringVarP(&optDir, "dir", "d", "", T("flag_dir"))
	rootCmd.PersistentFlags().StringVar(&optDuration, "keep-duration", "", T("flag_duration"))
	rootCmd.PersistentFlags().StringVar(&optSize, "keep-size", "", T("flag_size"))
	rootCmd.PersistentFlags().StringVar(&optLang, "lang", "", T("flag_lang"))

	var cleanCmd = &cobra.Command{
		Use:   "clean",
		Short: T("desc_clean"),
		Run: func(cmd *cobra.Command, args []string) {
			if err := performCleanup(); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		},
	}

	var daemonCmd = &cobra.Command{
		Use:   "daemon",
		Short: T("desc_daemon"),
		Run: func(cmd *cobra.Command, args []string) {
			if optDir == "" || (optSize == "" && optDuration == "") {
				fmt.Println(T("err_dir_req"), "/", T("err_keep_req"))
				os.Exit(1)
			}
			startDaemon()
		},
	}
	daemonCmd.Flags().StringVarP(&optInterval, "interval", "i", "30m", T("flag_interval"))

	var installCmd = &cobra.Command{
		Use:   "install",
		Short: T("desc_install"),
		Run: func(cmd *cobra.Command, args []string) {
			if optDir == "" || (optSize == "" && optDuration == "") {
				fmt.Println(T("err_dir_req"), "/", T("err_keep_req"))
				os.Exit(1)
			}
			if err := installService(); err != nil {
				fmt.Printf("Installation failed: %v\n", err)
				os.Exit(1)
			}
		},
	}
	installCmd.Flags().StringVarP(&optInterval, "interval", "i", "30m", T("flag_interval"))
	installCmd.Flags().StringVarP(&optMethod, "method", "m", "systemd", T("flag_method"))

	rootCmd.AddCommand(cleanCmd, daemonCmd, installCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
