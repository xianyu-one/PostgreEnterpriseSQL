package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

// PgInstance holds information about a detected PostgreSQL instance
type PgInstance struct {
	PID     int
	Type    string
	Port    string
	DataDir string
	Command string
}

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:     "pg_checker",
	Short:   "pg_checker is a tool to find running PostgreSQL instances",
	Long: `pg_checker scans the system processes to detect running PostgreSQL server instances.
It attempts to extract the port and data directory from the process command line.`,
	Version: Version,
	Run: func(cmd *cobra.Command, args []string) {
		runChecker()
	},
}

func runChecker() {
	fmt.Println("Scanning for PostgreSQL instances...")

	instances, err := findPgInstances()
	if err != nil {
		log.Fatalf("Error scanning processes: %v", err)
	}

	if len(instances) == 0 {
		fmt.Println("No PostgreSQL instances found running.")
		return
	}

	renderTable(instances)
}

// findPgInstances scans system processes for postgres
func findPgInstances() ([]PgInstance, error) {
	var instances []PgInstance

	// We'll use 'ps' command as a generic way to get processes across Unix-like systems
	// For pure Go cross-platform process listing without cgo, libraries like gopsutil are better,
	// but using exec for ps is straightforward for this purely standard library + specific requested deps approach.

	// ps -eo pid,command
	cmd := exec.Command("ps", "-eo", "pid,command")
	output, err := cmd.Output()
	if err != nil {
		// Fallback for systems where standard ps args might differ slightly or if we want to try reading /proc directly on Linux
		return findPgInstancesProcfs()
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		pidStr := parts[0]
		commandStr := strings.Join(parts[1:], " ")

		// Basic check: is it a postgres server process?
		// We look for 'postgres' executable, and usually we want the main process, not the background workers.
		// Often the main process command contains '-D' or is just 'postgres' without worker indicators.
		isValid, procType := classifyProcess(commandStr)
		if isValid {
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}

			instance := parsePgArgs(pid, procType, commandStr)
			instances = append(instances, instance)
		}
	}

	return instances, nil
}

// findPgInstancesProcfs is a fallback that reads /proc directly, works mainly on Linux
func findPgInstancesProcfs() ([]PgInstance, error) {
	var instances []PgInstance

	procDir, err := os.Open("/proc")
	if err != nil {
		return nil, fmt.Errorf("could not open /proc (are you on Windows or macOS?): %v", err)
	}
	defer procDir.Close()

	names, err := procDir.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		pid, err := strconv.Atoi(name)
		if err != nil {
			continue // Not a PID directory
		}

		cmdlinePath := filepath.Join("/proc", name, "cmdline")
		cmdlineBytes, err := os.ReadFile(cmdlinePath)
		if err != nil || len(cmdlineBytes) == 0 {
			continue
		}

		// cmdline is null-separated
		args := strings.Split(string(cmdlineBytes), "\x00")
		// Remove empty strings that might trail
		var validArgs []string
		for _, arg := range args {
			if arg != "" {
				validArgs = append(validArgs, arg)
			}
		}

		if len(validArgs) == 0 {
			continue
		}

		commandStr := strings.Join(validArgs, " ")

		isValid, procType := classifyProcess(commandStr)
		if isValid {
			instance := parsePgArgs(pid, procType, commandStr)
			instances = append(instances, instance)
		}
	}

	return instances, nil
}

func classifyProcess(cmd string) (bool, string) {
	// Extract base executable name roughly
	args := strings.Fields(cmd)
	if len(args) == 0 {
		return false, ""
	}

	exePath := args[0]
	baseExe := filepath.Base(exePath)

	// Identify repmgr
	if baseExe == "repmgrd" || strings.Contains(cmd, "repmgrd") {
		return true, "repmgr"
	}

	lowerCmd := strings.ToLower(cmd)
	isPg := strings.Contains(baseExe, "postgres") || strings.Contains(baseExe, "postmaster")

	if !isPg && !strings.Contains(lowerCmd, "postgres") && !strings.Contains(lowerCmd, "postmaster") {
		return false, ""
	}

	// Filter out common background processes that share the name
	exclusions := []string{
		"checkpointer",
		"background writer",
		"walwriter",
		"autovacuum launcher",
		"stats collector",
		"logical replication launcher",
		"postgres: logger",
		"postgres: checkpointer",
		"postgres: background writer",
		"postgres: walwriter",
		"postgres: autovacuum",
		"postgres: stats",
		"postgres: logical replication",
		"postgres: archiver",
		"postgres: walsender",
		"postgres: wal receiver",
	}

	for _, excl := range exclusions {
		if strings.Contains(lowerCmd, excl) {
			return false, ""
		}
	}

	// Filter out standard connections (usually look like "postgres: user db host port")
	if strings.Contains(cmd, "postgres: ") && !strings.Contains(cmd, "-D") {
		if !strings.Contains(cmd, "/") && !strings.Contains(cmd, "-") {
			return false, ""
		}
	}

	return true, "PostgreSQL"
}

func parsePgArgs(pid int, procType string, cmd string) PgInstance {
	inst := PgInstance{
		PID:     pid,
		Type:    procType,
		Command: cmd,
		Port:    "Unknown",
		DataDir: "Unknown",
	}

	// Tokenize the command string roughly
	args := strings.Fields(cmd)

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Look for Data Directory
		if arg == "-D" && i+1 < len(args) {
			inst.DataDir = args[i+1]
			i++ // skip next
		} else if strings.HasPrefix(arg, "--config-file=") {
			configFile := strings.TrimPrefix(arg, "--config-file=")
			inst.DataDir = filepath.Dir(configFile) + " (inferred from config)"
		}

		// Look for Port as fallback
		if arg == "-p" && i+1 < len(args) {
			inst.Port = args[i+1]
			i++
		} else if arg == "--port" && i+1 < len(args) {
			inst.Port = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--port=") {
			inst.Port = strings.TrimPrefix(arg, "--port=")
		}
	}

	// Attempt pure Go direct OS port detection
	detectedPort := detectPort(pid, inst.DataDir)
	if detectedPort != "" {
		inst.Port = detectedPort
	} else if inst.Port == "Unknown" {
		inst.Port = "Unknown (check config/perms)"
	}

	return inst
}

func detectPort(pid int, dataDir string) string {
	// Method 1: Pure Go OS-level socket detection mapping /proc/[pid]/fd -> /proc/net/tcp
	// Needs same-user or root permissions
	osPort := detectPortFromProc(pid)
	if osPort != "" {
		return osPort
	}

	// Method 2: If OS detection fails due to permissions, fallback to reading postmaster.pid
	if dataDir != "" && dataDir != "Unknown" && !strings.Contains(dataDir, "(inferred") {
		pidFile := filepath.Join(dataDir, "postmaster.pid")
		content, err := os.ReadFile(pidFile)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			if len(lines) >= 4 {
				portStr := strings.TrimSpace(lines[3])
				if portStr != "" {
					return portStr
				}
			}
		}
	}

	return ""
}

func detectPortFromProc(pid int) string {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	files, err := os.ReadDir(fdDir)
	if err != nil {
		return "" // Likely permission denied or process exited
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

	// Deduplicate in case of multiple sockets on same port (IPv4 + IPv6)
	portMap := make(map[string]bool)
	var uniquePorts []string
	for _, p := range ports {
		if !portMap[p] {
			portMap[p] = true
			uniquePorts = append(uniquePorts, p)
		}
	}

	if len(uniquePorts) > 0 {
		return strings.Join(uniquePorts, ", ")
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
		if len(fields) < 10 {
			continue
		}

		state := fields[3]
		if state != "0A" { // 0A is TCP_LISTEN
			continue
		}

		inode := fields[9]
		if inodes[inode] {
			localAddr := fields[1]
			parts := strings.Split(localAddr, ":")
			if len(parts) == 2 {
				portHex := parts[1]
				portDec, err := strconv.ParseInt(portHex, 16, 32)
				if err == nil {
					foundPorts = append(foundPorts, fmt.Sprintf("%d", portDec))
				}
			}
		}
	}
	return foundPorts
}

func renderTable(instances []PgInstance) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)

	// Set standard styling
	t.SetStyle(table.StyleLight)

	t.AppendHeader(table.Row{"PID", "Type", "Port", "Data Directory", "Command"})

	for _, inst := range instances {
		// Truncate command if it's too long
		cmdDisp := inst.Command
		if len(cmdDisp) > 80 {
			cmdDisp = cmdDisp[:77] + "..."
		}
		t.AppendRow(table.Row{inst.PID, inst.Type, inst.Port, inst.DataDir, cmdDisp})
		t.AppendSeparator()
	}

	t.Render()
}

func main() {
	// Set output to a discarder for log init, cobra handles its own output mostly
	log.SetOutput(io.Discard)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
