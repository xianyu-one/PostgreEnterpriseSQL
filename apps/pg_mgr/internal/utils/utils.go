package utils

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/chzyer/readline"
	"github.com/jedib0t/go-pretty/v6/text"
	"golang.org/x/term"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
)

func PromptInput(label string, defaultVal string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintf(os.Stderr, "%s [%s]: ", text.FgCyan.Sprint(label), text.FgGreen.Sprint(defaultVal))
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func PromptConfirm(label string) bool {
	return PromptBool(label, false)
}

func PromptBool(label string, defaultVal bool) bool {
	fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(label))
	fmt.Fprintf(os.Stderr, "  1. %s\n", i18n.T("option_yes"))
	fmt.Fprintf(os.Stderr, "  2. %s\n", i18n.T("option_no"))
	fmt.Fprintln(os.Stderr, "  0. Cancel")
	defaultIndex := 1
	if defaultVal {
		defaultIndex = 0
	}
	index, _ := PromptSelect(i18n.T("prompt_select_option"), 2, defaultIndex)
	return index == 0
}

// PromptSelect asks the user to select a numbered item. The returned index is zero-based.
func PromptSelect(label string, itemCount, defaultIndex int) (int, error) {
	if itemCount == 0 {
		return -1, errors.New("no selectable items")
	}
	if defaultIndex < 0 || defaultIndex >= itemCount {
		defaultIndex = 0
	}
	for {
		value := PromptInput(label, strconv.Itoa(defaultIndex+1))
		if value == "0" {
			return -1, interaction.ErrCancelled
		}
		index, err := strconv.Atoi(value)
		if err == nil && index >= 1 && index <= itemCount {
			return index - 1, nil
		}
		fmt.Fprintln(os.Stderr, text.FgHiRed.Sprint(i18n.T("menu_valid_range_legacy", itemCount)))
	}
}

// PromptPath provides filesystem completion when stdin is an interactive terminal.
func PromptPath(label, defaultVal string) string {
	prompt := fmt.Sprintf("%s [%s]: ", text.FgCyan.Sprint(label), text.FgGreen.Sprint(defaultVal))
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return PromptInput(label, defaultVal)
	}
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          prompt,
		AutoComplete:    pathCompleter{},
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		HistoryLimit:    -1,
	})
	if err != nil {
		return PromptInput(label, defaultVal)
	}
	defer rl.Close()
	value, err := rl.Readline()
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultVal
	}
	return expandHome(strings.TrimSpace(value))
}

// PromptNewPassword reads a password without echo and requires confirmation.
func PromptNewPassword(label, confirmLabel, mismatchMessage string) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		first, err := readSecret(reader, label)
		if err != nil {
			return "", err
		}
		second, err := readSecret(reader, confirmLabel)
		if err != nil {
			return "", err
		}
		if first == second {
			return first, nil
		}
		fmt.Println(text.FgHiRed.Sprint(mismatchMessage))
	}
}

func readSecret(reader *bufio.Reader, label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", text.FgCyan.Sprint(label))
	if term.IsTerminal(int(os.Stdin.Fd())) {
		value, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(value), err
	}
	value, err := reader.ReadString('\n')
	return strings.TrimRight(value, "\r\n"), err
}

type pathCompleter struct{}

func (pathCompleter) Do(line []rune, pos int) ([][]rune, int) {
	typed := string(line[:pos])
	expanded := expandHome(typed)
	dir, prefix := filepath.Split(expanded)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0
	}
	var candidates [][]rune
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		suffix := strings.TrimPrefix(entry.Name(), prefix)
		if entry.IsDir() {
			suffix += string(os.PathSeparator)
		}
		candidates = append(candidates, []rune(suffix))
	}
	return candidates, len([]rune(prefix))
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

// ReadSelection is exposed for tests and non-terminal callers that need the same validation.
func ReadSelection(reader io.Reader, itemCount int) (int, error) {
	value, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return -1, err
	}
	index, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || index < 1 || index > itemCount {
		return -1, fmt.Errorf("selection must be between 1 and %d", itemCount)
	}
	return index - 1, nil
}

func ReplaceInFile(filepath string, pattern string, replacement string) error {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return err
	}
	re := regexp.MustCompile(pattern)
	newContent := re.ReplaceAllLiteral(content, []byte(replacement))
	return os.WriteFile(filepath, newContent, 0644)
}

func AppendToFile(filepath string, text string) error {
	f, err := os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return err
}

func RunCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, stderr.String())
	}
	return nil
}

// DetectLogindRemoveIPC returns the effective RemoveIPC setting reported by
// systemd. With no explicit setting, systemd's documented default is "yes".
func DetectLogindRemoveIPC() (string, error) {
	output, err := exec.Command("systemd-analyze", "cat-config", "systemd/logind.conf").Output()
	if err != nil {
		return "unknown", fmt.Errorf("systemd-analyze cat-config failed: %w", err)
	}
	return ParseLogindRemoveIPC(string(output)), nil
}

// ParseLogindRemoveIPC parses merged systemd-analyze cat-config output.
func ParseLogindRemoveIPC(content string) string {
	setting := "yes"
	inLoginSection := false

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inLoginSection = strings.EqualFold(line, "[Login]")
			continue
		}
		if !inLoginSection {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "RemoveIPC") {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "no", "false", "off", "0":
			setting = "no"
		case "yes", "true", "on", "1":
			setting = "yes"
		default:
			setting = "unknown"
		}
	}
	return setting
}

func RunAsUser(username string, cmdStr string) error {
	currUser, err := GetCurrentOSUser()
	if err == nil && currUser == username {
		out, runErr := exec.Command("bash", "-c", cmdStr).CombinedOutput()
		if runErr != nil {
			return fmt.Errorf("%v: %s", runErr, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// Use -s /bin/bash to ensure execution works even if the user has /bin/false or /usr/sbin/nologin
	out, runErr := exec.Command("su", "-s", "/bin/bash", "-", username, "-c", cmdStr).CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("%v: %s", runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

func RunAsUserWithOutput(username string, cmdStr string) (string, error) {
	currUser, err := GetCurrentOSUser()
	if err == nil && currUser == username {
		cmd := exec.Command("bash", "-c", cmdStr)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	cmd := exec.Command("su", "-s", "/bin/bash", "-", username, "-c", cmdStr)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// RunAsUserWithCombinedOutput runs a shell command as username and returns both
// stdout and stderr. When the caller already is username, it executes directly
// and never invokes su (which would unnecessarily require authentication).
func RunAsUserWithCombinedOutput(username string, cmdStr string) (string, error) {
	currUser, err := GetCurrentOSUser()
	if err == nil && currUser == username {
		out, runErr := exec.Command("bash", "-c", cmdStr).CombinedOutput()
		return string(out), runErr
	}
	out, runErr := exec.Command("su", "-s", "/bin/bash", "-", username, "-c", cmdStr).CombinedOutput()
	return string(out), runErr
}

// RunAsUserWithLiveOutput streams a command's combined stdout/stderr to writer
// while retaining the same output for error reporting.
func RunAsUserWithLiveOutput(username, cmdStr string, writer io.Writer) (string, error) {
	if writer == nil {
		writer = io.Discard
	}
	var captured bytes.Buffer
	combined := io.MultiWriter(writer, &captured)
	currUser, err := GetCurrentOSUser()
	var cmd *exec.Cmd
	if err == nil && currUser == username {
		cmd = exec.Command("bash", "-c", cmdStr)
	} else {
		cmd = exec.Command("su", "-s", "/bin/bash", "-", username, "-c", cmdStr)
	}
	cmd.Stdout = combined
	cmd.Stderr = combined
	runErr := cmd.Run()
	return captured.String(), runErr
}

func ExtractRegexFromFile(filepath string, pattern string) string {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(string(content))
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func UntarGz(gzipStream io.Reader, targetDir string, uid, gid int) error {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		return err
	}
	defer uncompressedStream.Close()

	tarReader := tar.NewReader(uncompressedStream)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, header.Name)
		if !strings.HasPrefix(target, filepath.Clean(targetDir)+string(os.PathSeparator)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
			os.Chown(target, uid, gid)
		case tar.TypeReg:
			dir := filepath.Dir(target)
			os.MkdirAll(dir, 0755)
			os.Chown(dir, uid, gid)
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
			os.Chown(target, uid, gid)
		case tar.TypeSymlink:
			os.Symlink(header.Linkname, target)
		case tar.TypeLink:
			linkTarget := filepath.Join(targetDir, header.Linkname)
			if !strings.HasPrefix(linkTarget, filepath.Clean(targetDir)+string(os.PathSeparator)) {
				return fmt.Errorf("hard link target escapes extraction directory: %s", header.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := os.Link(linkTarget, target); err != nil {
				return err
			}
			if err := os.Chown(target, uid, gid); err != nil {
				return err
			}
		}
	}
	return nil
}

func DetectVersionFromTar(tarPath string) (major string, minor string, ok bool) {
	filename := filepath.Base(tarPath)
	reList := []*regexp.Regexp{
		regexp.MustCompile(`(?:postgresql|postgres|pg)-(\d+)\.(\d+)`),
		regexp.MustCompile(`^(\d+)\.(\d+)`),
		regexp.MustCompile(`-(\d+)\.(\d+)`),
	}
	for _, re := range reList {
		matches := re.FindStringSubmatch(filename)
		if len(matches) >= 3 {
			return matches[1], matches[2], true
		}
	}
	return "", "", false
}

func UpdatePgrc(pgrcPath string, envs map[string]string) error {
	var lines []string

	contentBytes, err := os.ReadFile(pgrcPath)
	if err == nil {
		content := string(contentBytes)
		rawLines := strings.Split(content, "\n")

		updated := make(map[string]bool)
		re := regexp.MustCompile(`^[ \t]*export[ \t]+([A-Za-z0-9_]+)[ \t]*=`)

		for _, line := range rawLines {
			trimmed := strings.TrimSpace(line)
			matches := re.FindStringSubmatch(trimmed)
			if len(matches) > 1 {
				key := matches[1]
				if val, ok := envs[key]; ok {
					lines = append(lines, fmt.Sprintf("export %s=%s", key, val))
					updated[key] = true
					continue
				}
			}
			lines = append(lines, line)
		}

		// Append any variables that were not in the original file
		for k, v := range envs {
			if !updated[k] {
				lines = append(lines, fmt.Sprintf("export %s=%s", k, v))
			}
		}

	} else if os.IsNotExist(err) {
		for k, v := range envs {
			lines = append(lines, fmt.Sprintf("export %s=%s", k, v))
		}
	} else {
		return err
	}

	newContent := strings.Join(lines, "\n")
	if newContent != "" && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	return os.WriteFile(pgrcPath, []byte(newContent), 0644)
}

func UpdatePostgresqlConfParam(filePath string, name string, val string) error {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	content := string(contentBytes)

	formattedVal := val
	if (strings.ContainsAny(val, " \t;&|<>") || strings.Contains(val, " ")) && !strings.HasPrefix(val, "'") && !strings.HasPrefix(val, "\"") {
		formattedVal = fmt.Sprintf("'%s'", strings.ReplaceAll(val, "'", "''"))
	}

	rePattern := `(?m)^[ \t]*` + regexp.QuoteMeta(name) + `[ \t]*=.*$`
	re := regexp.MustCompile(rePattern)
	if re.MatchString(content) {
		newContent := re.ReplaceAllLiteralString(content, fmt.Sprintf("%s = %s", name, formattedVal))
		return os.WriteFile(filePath, []byte(newContent), 0644)
	}

	return AppendToFile(filePath, fmt.Sprintf("\n%s = %s\n", name, formattedVal))
}

func GetPostgresqlConfParam(filePath string, name string) (string, bool) {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", false
	}
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(name) + `[ \t]*=[ \t]*(.+)`)
	matches := re.FindStringSubmatch(string(contentBytes))
	if len(matches) < 2 {
		return "", false
	}
	rawVal := strings.TrimSpace(matches[1])
	if strings.HasPrefix(rawVal, "'") {
		idx := strings.LastIndex(rawVal, "'")
		if idx > 0 {
			rawVal = rawVal[1:idx]
		}
	} else if strings.HasPrefix(rawVal, "\"") {
		idx := strings.LastIndex(rawVal, "\"")
		if idx > 0 {
			rawVal = rawVal[1:idx]
		}
	} else {
		if idx := strings.Index(rawVal, "#"); idx != -1 {
			rawVal = strings.TrimSpace(rawVal[:idx])
		}
	}
	return rawVal, true
}

const (
	PgMgrArchiveStartMarker = "true PG_MGR_ARCHIVE_START"
	PgMgrArchiveEndMarker   = "true PG_MGR_ARCHIVE_END"
)

func ParseArchiveCommand(rawCmd string) (userPart string, pgMgrPart string) {
	rawCmd = strings.TrimSpace(rawCmd)
	if strings.HasPrefix(rawCmd, "'") && strings.HasSuffix(rawCmd, "'") && len(rawCmd) >= 2 {
		rawCmd = rawCmd[1 : len(rawCmd)-1]
	}

	startIdx := strings.Index(rawCmd, PgMgrArchiveStartMarker)
	endIdx := strings.Index(rawCmd, PgMgrArchiveEndMarker)

	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		semiIdx := strings.Index(rawCmd[startIdx:], ";")
		if semiIdx != -1 {
			cmdStart := startIdx + semiIdx + 1
			pgMgrPart = strings.TrimSpace(rawCmd[cmdStart:endIdx])
			for {
				trimmed := strings.TrimSpace(pgMgrPart)
				if strings.HasSuffix(trimmed, ";") {
					pgMgrPart = strings.TrimSuffix(trimmed, ";")
				} else if strings.HasSuffix(trimmed, "&&") {
					pgMgrPart = strings.TrimSuffix(trimmed, "&&")
				} else {
					pgMgrPart = trimmed
					break
				}
			}
		}

		blockStart := startIdx
		blockEnd := endIdx + len(PgMgrArchiveEndMarker)

		left := rawCmd[:blockStart]
		right := rawCmd[blockEnd:]

		left = strings.TrimSpace(left)
		left = strings.TrimSuffix(left, ";")
		left = strings.TrimSpace(left)

		right = strings.TrimSpace(right)
		right = strings.TrimPrefix(right, ";")
		right = strings.TrimSpace(right)

		if left != "" && right != "" {
			userPart = left + " ; " + right
		} else if left != "" {
			userPart = left
		} else {
			userPart = right
		}
		return userPart, pgMgrPart
	}

	return rawCmd, ""
}

func BuildArchiveCommand(userPart string, newPgMgrCmd string) string {
	userPart = strings.TrimSpace(userPart)
	userPart = strings.TrimSuffix(userPart, ";")
	userPart = strings.TrimSpace(userPart)

	newPgMgrCmd = strings.TrimSpace(newPgMgrCmd)

	if newPgMgrCmd == "" {
		return userPart
	}

	tagBlock := fmt.Sprintf("true PG_MGR_ARCHIVE_START ; %s && true PG_MGR_ARCHIVE_END", newPgMgrCmd)

	if userPart == "" {
		return tagBlock
	}

	return fmt.Sprintf("%s ; %s", userPart, tagBlock)
}

func ExtractArchiveDirFromCmd(pgMgrCmd string) string {
	pgMgrCmd = strings.TrimSpace(pgMgrCmd)
	if pgMgrCmd == "" {
		return ""
	}

	reExport := regexp.MustCompile(`export[ \t]+PG_ARCHDIR=['"]?([^'\";&\s]+)['"]?`)
	matches := reExport.FindStringSubmatch(pgMgrCmd)
	if len(matches) > 1 {
		return filepath.Clean(matches[1])
	}

	reCp := regexp.MustCompile(`cp[ \t]+%p[ \t]+([^;\&\s]+)/%f`)
	matches = reCp.FindStringSubmatch(pgMgrCmd)
	if len(matches) > 1 {
		return filepath.Clean(matches[1])
	}

	reTest := regexp.MustCompile(`test[ \t]+![ \t]+-f[ \t]+([^;\&\s]+)/%f`)
	matches = reTest.FindStringSubmatch(pgMgrCmd)
	if len(matches) > 1 {
		return filepath.Clean(matches[1])
	}

	return ""
}

func GetPgMgrArchiveDir(confPath string) string {
	fullCmd, ok := GetPostgresqlConfParam(confPath, "archive_command")
	if !ok || fullCmd == "" {
		return ""
	}
	_, pgMgrPart := ParseArchiveCommand(fullCmd)
	if pgMgrPart == "" {
		return ""
	}
	return ExtractArchiveDirFromCmd(pgMgrPart)
}

func GetInstanceBinDir(meta config.InstanceMeta) string {
	if meta.BinPath == "" {
		return ""
	}
	binDir := meta.BinPath
	fi, err := os.Stat(meta.BinPath)
	if err == nil {
		if !fi.IsDir() {
			binDir = filepath.Dir(meta.BinPath)
		}
	} else {
		base := filepath.Base(meta.BinPath)
		if strings.Contains(base, ".") || base == "postgres" || base == "pg_ctl" || base == "pg_rman" || base == "psql" {
			binDir = filepath.Dir(meta.BinPath)
		}
	}
	return binDir
}

func GetPgrmanBin(meta config.InstanceMeta) string {
	binDir := GetInstanceBinDir(meta)
	if binDir != "" {
		candidate := filepath.Join(binDir, "pg_rman")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "pg_rman"
}

func GetInstanceEnvPrefix(meta config.InstanceMeta) string {
	var exports []string
	binDir := GetInstanceBinDir(meta)
	if binDir != "" {
		exports = append(exports, fmt.Sprintf("export PATH=%s:$PATH", binDir))
		versionDir := filepath.Dir(binDir)
		libDir := filepath.Join(versionDir, "lib")
		if _, err := os.Stat(libDir); err == nil {
			exports = append(exports, fmt.Sprintf("export LD_LIBRARY_PATH=%s:$LD_LIBRARY_PATH", libDir))
		} else if _, err2 := os.Stat(filepath.Join(binDir, "lib")); err2 == nil {
			exports = append(exports, fmt.Sprintf("export LD_LIBRARY_PATH=%s:$LD_LIBRARY_PATH", filepath.Join(binDir, "lib")))
		}
		exports = append(exports, fmt.Sprintf("export PG_VERSION_PATH=%s", versionDir))
	}
	if meta.DataDir != "" {
		exports = append(exports, fmt.Sprintf("export PGDATA=%s", meta.DataDir))
	}
	if meta.Port != "" {
		exports = append(exports, fmt.Sprintf("export PGPORT=%s", meta.Port))
	}
	if meta.Pgrman != nil && meta.Pgrman.BackupDir != "" {
		exports = append(exports, fmt.Sprintf("export PG_RMAN_BACK_PATH=%s", meta.Pgrman.BackupDir))
	}
	if len(exports) == 0 {
		return ""
	}
	return strings.Join(exports, " && ")
}

func BuildInstanceCmd(meta config.InstanceMeta, rawCmdStr string) string {
	prefix := GetInstanceEnvPrefix(meta)
	if prefix == "" {
		return rawCmdStr
	}
	return prefix + " && " + rawCmdStr
}

func RunAsUserForInstance(username string, meta config.InstanceMeta, cmdStr string) error {
	if username == "" {
		username = meta.User
	}
	fullCmd := BuildInstanceCmd(meta, cmdStr)
	return RunAsUser(username, fullCmd)
}

func RunAsUserWithOutputForInstance(username string, meta config.InstanceMeta, cmdStr string) (string, error) {
	if username == "" {
		username = meta.User
	}
	fullCmd := BuildInstanceCmd(meta, cmdStr)
	return RunAsUserWithOutput(username, fullCmd)
}

func RunAsUserWithCombinedOutputForInstance(username string, meta config.InstanceMeta, cmdStr string) (string, error) {
	if username == "" {
		username = meta.User
	}
	fullCmd := BuildInstanceCmd(meta, cmdStr)
	return RunAsUserWithCombinedOutput(username, fullCmd)
}

// isSubpathOrEqual checks whether child is equal to parent or is a subdirectory inside parent.
func isSubpathOrEqual(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

// CopyDir recursively copies directory structure and files from src to dst.
func CopyDir(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	return copyDirInternal(src, dst, src, dst)
}

func copyDirInternal(src, dst, topSrc, topDst string) error {
	if src == dst {
		return nil
	}

	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("source '%s' is not a directory", src)
	}

	if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
		return err
	}

	topDstFi, _ := os.Stat(topDst)
	topSrcFi, _ := os.Stat(topSrc)

	dstInsideSrc := isSubpathOrEqual(topSrc, topDst) && topSrc != topDst
	srcInsideDst := isSubpathOrEqual(topDst, topSrc) && topSrc != topDst

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if srcPath == dstPath {
			continue
		}

		if dstInsideSrc {
			if isSubpathOrEqual(topDst, srcPath) {
				continue
			}
			if topDstFi != nil {
				if srcFi, err := os.Stat(srcPath); err == nil && os.SameFile(srcFi, topDstFi) {
					continue
				}
			}
		}

		if srcInsideDst {
			if isSubpathOrEqual(topSrc, dstPath) {
				continue
			}
			if topSrcFi != nil {
				if dstFi, err := os.Stat(dstPath); err == nil && os.SameFile(dstFi, topSrcFi) {
					continue
				}
			}
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			_ = os.Remove(dstPath)
			if err := os.Symlink(linkTarget, dstPath); err != nil {
				return err
			}
		} else if info.IsDir() {
			if err := copyDirInternal(srcPath, dstPath, topSrc, topDst); err != nil {
				return err
			}
		} else {
			if err := CopyFile(srcPath, dstPath, info.Mode()); err != nil {
				return err
			}
		}

		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			_ = os.Chown(dstPath, int(stat.Uid), int(stat.Gid))
		}
		_ = os.Chtimes(dstPath, info.ModTime(), info.ModTime())
	}

	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(dst, int(stat.Uid), int(stat.Gid))
	}
	_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())

	return nil
}

// CopyFile copies a single file from src to dst preserving file mode.
func CopyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// MigrateDirectory moves or copies directory contents from oldDir to newDir.
func MigrateDirectory(oldDir, newDir string) error {
	oldDir = filepath.Clean(oldDir)
	newDir = filepath.Clean(newDir)

	if oldDir == newDir || oldDir == "" || newDir == "" {
		return nil
	}

	fi, err := os.Stat(oldDir)
	if os.IsNotExist(err) {
		return fmt.Errorf("source directory '%s' does not exist", oldDir)
	} else if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("source '%s' is not a directory", oldDir)
	}

	if err := os.MkdirAll(filepath.Dir(newDir), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory for '%s': %v", newDir, err)
	}

	err = os.Rename(oldDir, newDir)
	if err == nil {
		return nil
	}

	if err := os.MkdirAll(newDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory '%s': %v", newDir, err)
	}

	if err := CopyDir(oldDir, newDir); err != nil {
		return fmt.Errorf("failed to copy files from '%s' to '%s': %v", oldDir, newDir, err)
	}

	if isSubpathOrEqual(oldDir, newDir) {
		entries, err := os.ReadDir(oldDir)
		if err == nil {
			newDirFi, _ := os.Stat(newDir)
			for _, entry := range entries {
				itemPath := filepath.Join(oldDir, entry.Name())
				if isSubpathOrEqual(itemPath, newDir) {
					continue
				}
				if newDirFi != nil {
					if itemFi, err := os.Stat(itemPath); err == nil && os.SameFile(itemFi, newDirFi) {
						continue
					}
				}
				_ = os.RemoveAll(itemPath)
			}
		}
	} else {
		if err := os.RemoveAll(oldDir); err != nil {
			fmt.Printf("Warning: failed to remove old directory '%s': %v\n", oldDir, err)
		}
	}

	return nil
}
