package process

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pg_mgr/internal/utils"
)

type PgProcess struct {
	PID     string
	OSUser  string
	Port    string
	DataDir string
	Command string
	BinPath string
}

func FindPgProcesses() []PgProcess {
	var instances []PgProcess

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return instances
	}

	userCache := make(map[string]string)
	getUser := func(uidStr string) string {
		if u, ok := userCache[uidStr]; ok {
			return u
		}
		uObj, err := user.LookupId(uidStr)
		if err == nil && uObj.Username != "" {
			userCache[uidStr] = uObj.Username
			return uObj.Username
		}
		userCache[uidStr] = uidStr
		return uidStr
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pidStr := entry.Name()
		if _, err := strconv.Atoi(pidStr); err != nil {
			continue // Not a PID directory
		}

		cmdlineBytes, err := os.ReadFile(filepath.Join("/proc", pidStr, "cmdline"))
		if err != nil || len(cmdlineBytes) == 0 {
			continue
		}

		commandStr := strings.TrimSpace(strings.ReplaceAll(string(cmdlineBytes), "\x00", " "))
		if commandStr == "" {
			continue
		}

		if isValidPg(commandStr) {
			userStr := "unknown"
			if statusBytes, err := os.ReadFile(filepath.Join("/proc", pidStr, "status")); err == nil {
				for _, line := range strings.Split(string(statusBytes), "\n") {
					if strings.HasPrefix(line, "Uid:") {
						fields := strings.Fields(line)
						if len(fields) >= 2 {
							userStr = getUser(fields[1])
						}
						break
					}
				}
			}

			inst := parsePgProcess(pidStr, userStr, commandStr)
			if inst.DataDir != "" && inst.DataDir != "Unknown" {
				instances = append(instances, inst)
			}
		}
	}

	return instances
}

func isValidPg(cmd string) bool {
	args := strings.Fields(cmd)
	if len(args) == 0 {
		return false
	}
	baseExe := filepath.Base(args[0])
	lowerCmd := strings.ToLower(cmd)
	isPg := strings.Contains(baseExe, "postgres") || strings.Contains(baseExe, "postmaster")

	if !isPg && !strings.Contains(lowerCmd, "postgres") && !strings.Contains(lowerCmd, "postmaster") {
		return false
	}
	exclusions := []string{
		"checkpointer", "background writer", "walwriter", "autovacuum",
		"stats collector", "logical replication", "logger", "archiver", "walsender", "wal receiver",
	}
	for _, excl := range exclusions {
		if strings.Contains(lowerCmd, excl) {
			return false
		}
	}
	if strings.Contains(cmd, "postgres: ") && !strings.Contains(cmd, "-D") {
		if !strings.Contains(cmd, "/") && !strings.Contains(cmd, "-") {
			return false
		}
	}
	return true
}

func parsePgProcess(pid string, osUser string, cmd string) PgProcess {
	inst := PgProcess{
		PID:     pid,
		OSUser:  osUser,
		Command: cmd,
		Port:    "Unknown",
		DataDir: "Unknown",
	}
	args := strings.Fields(cmd)
	if len(args) > 0 {
		inst.BinPath = args[0]
	}

	pidInt, _ := strconv.Atoi(pid)
	exePath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pidInt))
	if err == nil {
		inst.BinPath = exePath
	}

	for i := 0; i < len(args); i++ {
		if args[i] == "-D" && i+1 < len(args) {
			inst.DataDir = filepath.Clean(args[i+1])
		} else if strings.HasPrefix(args[i], "--config-file=") {
			inst.DataDir = filepath.Clean(filepath.Dir(strings.TrimPrefix(args[i], "--config-file=")))
		} else if args[i] == "-p" && i+1 < len(args) {
			inst.Port = args[i+1]
		}
	}

	detectedPort := detectPortFromProc(pidInt)
	if detectedPort != "" {
		inst.Port = detectedPort
	} else if inst.DataDir != "Unknown" {
		portStr := utils.ExtractRegexFromFile(filepath.Join(inst.DataDir, "postgresql.conf"), `(?m)^port\s*=\s*(\d+)`)
		if portStr != "" {
			inst.Port = portStr
		}
	}
	return inst
}

func detectPortFromProc(pid int) string {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	files, err := os.ReadDir(fdDir)
	if err != nil {
		return ""
	}
	socketInodes := make(map[string]bool)
	for _, f := range files {
		target, err := os.Readlink(filepath.Join(fdDir, f.Name()))
		if err == nil && strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inode := target[8 : len(target)-1]
			socketInodes[inode] = true
		}
	}
	if len(socketInodes) == 0 {
		return ""
	}
	var ports []string
	ports = append(ports, findPortsInNetFile("/proc/net/tcp", socketInodes)...)
	ports = append(ports, findPortsInNetFile("/proc/net/tcp6", socketInodes)...)

	if len(ports) > 0 {
		return ports[0]
	}
	return ""
}

type ProcInfo struct {
	PID     string
	PPID    string
	CPU     float64
	RSS     int64 // in KB
	Command string
}

func getSystemUptime() float64 {
	content, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(content))
	if len(fields) > 0 {
		val, err := strconv.ParseFloat(fields[0], 64)
		if err == nil {
			return val
		}
	}
	return 0
}

func readProcList() []ProcInfo {
	var procs []ProcInfo
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return procs
	}

	systemUptime := getSystemUptime()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pidStr := entry.Name()
		if _, err := strconv.Atoi(pidStr); err != nil {
			continue
		}

		cmdlineBytes, err := os.ReadFile(filepath.Join("/proc", pidStr, "cmdline"))
		cmdStr := ""
		if err == nil && len(cmdlineBytes) > 0 {
			cmdStr = strings.TrimSpace(strings.ReplaceAll(string(cmdlineBytes), "\x00", " "))
		}

		statBytes, err := os.ReadFile(filepath.Join("/proc", pidStr, "stat"))
		if err != nil {
			continue
		}
		statStr := string(statBytes)
		lastParen := strings.LastIndex(statStr, ")")
		if lastParen == -1 || lastParen+2 >= len(statStr) {
			continue
		}

		fields := strings.Fields(statStr[lastParen+2:])
		if len(fields) < 20 {
			continue
		}

		ppid := fields[1]
		utime, _ := strconv.ParseFloat(fields[11], 64)
		stime, _ := strconv.ParseFloat(fields[12], 64)
		starttime, _ := strconv.ParseFloat(fields[19], 64)

		if cmdStr == "" {
			firstParen := strings.Index(statStr, "(")
			if firstParen != -1 && firstParen < lastParen {
				cmdStr = statStr[firstParen+1 : lastParen]
			}
		}

		var rssVal int64
		if statusBytes, err := os.ReadFile(filepath.Join("/proc", pidStr, "status")); err == nil {
			for _, line := range strings.Split(string(statusBytes), "\n") {
				if strings.HasPrefix(line, "VmRSS:") {
					statusFields := strings.Fields(line)
					if len(statusFields) >= 2 {
						rssVal, _ = strconv.ParseInt(statusFields[1], 10, 64)
					}
					break
				}
			}
		}
		if rssVal == 0 && len(fields) >= 22 {
			pages, _ := strconv.ParseInt(fields[21], 10, 64)
			rssVal = pages * 4
		}

		var cpuVal float64
		if systemUptime > 0 {
			processSeconds := systemUptime - (starttime / 100.0)
			if processSeconds > 0 {
				totalTimeSeconds := (utime + stime) / 100.0
				cpuVal = (totalTimeSeconds / processSeconds) * 100.0
			}
		}

		procs = append(procs, ProcInfo{
			PID:     pidStr,
			PPID:    ppid,
			CPU:     cpuVal,
			RSS:     rssVal,
			Command: cmdStr,
		})
	}

	return procs
}

func GetInstanceResourceUsage(dataDir string) (string, string) {
	if dataDir == "" || dataDir == "Unknown" {
		return "0.0%", "0 B"
	}
	cleanTargetDir := filepath.Clean(dataDir)

	procs := readProcList()
	if len(procs) == 0 {
		return "0.0%", "0 B"
	}

	// Step 1: Find main PID for the dataDir
	var mainPID string

	// Option A: Try postmaster.pid file
	pmPidPath := filepath.Join(cleanTargetDir, "postmaster.pid")
	pmContent, err := os.ReadFile(pmPidPath)
	if err == nil {
		pmLines := strings.Split(string(pmContent), "\n")
		if len(pmLines) > 0 {
			candidate := strings.TrimSpace(pmLines[0])
			if _, err := strconv.Atoi(candidate); err == nil {
				// Verify candidate PID exists in ps list
				for _, p := range procs {
					if p.PID == candidate {
						mainPID = candidate
						break
					}
				}
			}
		}
	}

	// Option B: Search ps commands if postmaster.pid not matched
	if mainPID == "" {
		for _, p := range procs {
			if isValidPg(p.Command) {
				args := strings.Fields(p.Command)
				for i := 0; i < len(args); i++ {
					if args[i] == "-D" && i+1 < len(args) {
						if filepath.Clean(args[i+1]) == cleanTargetDir {
							mainPID = p.PID
							break
						}
					} else if strings.HasPrefix(args[i], "--config-file=") {
						cfgPath := strings.TrimPrefix(args[i], "--config-file=")
						if filepath.Clean(filepath.Dir(cfgPath)) == cleanTargetDir {
							mainPID = p.PID
							break
						}
					}
				}
				if mainPID != "" {
					break
				}
			}
		}
	}

	if mainPID == "" {
		return "0.0%", "0 B"
	}

	// Step 2: Collect mainPID and all child PIDs
	pidSet := map[string]bool{mainPID: true}
	added := true
	for added {
		added = false
		for _, p := range procs {
			if !pidSet[p.PID] && pidSet[p.PPID] {
				pidSet[p.PID] = true
				added = true
			}
		}
	}

	// Step 3: Aggregate CPU and RSS
	var totalCPU float64
	var totalRSS int64
	for _, p := range procs {
		if pidSet[p.PID] {
			totalCPU += p.CPU
			totalRSS += p.RSS
		}
	}

	cpuStr := fmt.Sprintf("%.1f%%", totalCPU)
	memStr := formatKB(totalRSS)
	return cpuStr, memStr
}

func formatKB(kb int64) string {
	if kb <= 0 {
		return "0 B"
	}
	bytes := float64(kb * 1024)
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(kb))
	} else if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", bytes/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", bytes/(1024*1024*1024))
}

func findPortsInNetFile(path string, inodes map[string]bool) []string {
	var foundPorts []string
	content, err := os.ReadFile(path)
	if err != nil {
		return foundPorts
	}
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[3] != "0A" {
			continue
		}
		if inodes[fields[9]] {
			parts := strings.Split(fields[1], ":")
			if len(parts) == 2 {
				portDec, err := strconv.ParseInt(parts[1], 16, 32)
				if err == nil {
					foundPorts = append(foundPorts, fmt.Sprintf("%d", portDec))
				}
			}
		}
	}
	return foundPorts
}

func GetInstanceUptime(dataDir string, fallbackPID ...string) string {
	var pidStr string
	if len(fallbackPID) > 0 {
		pidStr = fallbackPID[0]
	}

	if dataDir != "" && dataDir != "Unknown" {
		cleanDir := filepath.Clean(dataDir)
		pmPidPath := filepath.Join(cleanDir, "postmaster.pid")
		content, err := os.ReadFile(pmPidPath)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			if len(lines) >= 3 {
				pCandidate := strings.TrimSpace(lines[0])
				timeCandidate := strings.TrimSpace(lines[2])
				if sec, err := strconv.ParseInt(timeCandidate, 10, 64); err == nil && sec > 0 {
					if _, err := os.Stat(fmt.Sprintf("/proc/%s", pCandidate)); err == nil {
						startTime := time.Unix(sec, 0)
						return FormatDuration(time.Since(startTime))
					}
				}
				if pidStr == "" {
					pidStr = pCandidate
				}
			}
		}
	}

	if pidStr != "" && pidStr != "Unknown" {
		if fi, err := os.Stat(fmt.Sprintf("/proc/%s", pidStr)); err == nil {
			return FormatDuration(time.Since(fi.ModTime()))
		}
	}

	return "-"
}

func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	seconds := int((d - time.Duration(minutes)*time.Minute) / time.Second)

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
