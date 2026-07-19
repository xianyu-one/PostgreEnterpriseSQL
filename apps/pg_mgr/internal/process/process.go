package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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
	out, err := exec.Command("ps", "-eo", "pid,user,command").Output()
	if err != nil {
		return instances
	}

	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // skip header
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		pidStr := parts[0]
		userStr := parts[1]
		commandStr := strings.Join(parts[2:], " ")

		if isValidPg(commandStr) {
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
