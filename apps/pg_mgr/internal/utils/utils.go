package utils

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
)

func PromptInput(label string, defaultVal string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [%s]: ", text.FgCyan.Sprint(label), text.FgGreen.Sprint(defaultVal))
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func PromptConfirm(label string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s ", text.FgHiYellow.Sprint(label))
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
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

func RunAsUser(username string, cmdStr string) error {
	// Use -s /bin/bash to ensure execution works even if the user has /bin/false or /usr/sbin/nologin
	cmd := exec.Command("su", "-s", "/bin/bash", "-", username, "-c", cmdStr)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, stderr.String())
	}
	return nil
}

func RunAsUserWithOutput(username string, cmdStr string) (string, error) {
	cmd := exec.Command("su", "-s", "/bin/bash", "-", username, "-c", cmdStr)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
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
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
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
