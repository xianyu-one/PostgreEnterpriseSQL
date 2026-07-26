package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jedib0t/go-pretty/v6/text"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

func init() {
	config.PrivilegedWriteFunc = promptPrivilegedConfigWrite
}

func promptPrivilegedConfigWrite(targetPath string, content []byte) error {
	if utils.IsRoot() {
		return fmt.Errorf("permission denied writing %s", targetPath)
	}

	fmt.Println(text.FgHiYellow.Sprint(i18n.T("config_write_needs_privilege", targetPath)))
	fmt.Println(i18n.T("privilege_option_sudo"))
	fmt.Println(i18n.T("privilege_option_su"))
	fmt.Println(i18n.T("privilege_option_cancel"))
	choice := utils.PromptInput(i18n.T("prompt_privilege_method"), "1")
	if choice != "1" && choice != "2" {
		return fmt.Errorf("%s", i18n.T("privilege_cancelled"))
	}

	tempFile, err := os.CreateTemp("", "pg_mgr-privileged-conf-*.yaml")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if err := tempFile.Chmod(0600); err != nil {
		tempFile.Close()
		return err
	}
	if _, err := tempFile.Write(content); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	parentDir := filepath.Dir(targetPath)
	if choice == "1" {
		if err := runInteractiveCommand("sudo", "install", "-d", "-m", "0755", parentDir); err != nil {
			return err
		}
		return runInteractiveCommand("sudo", "install", "-m", "0644", tempPath, targetPath)
	}

	command := fmt.Sprintf("install -d -m 0755 %s && install -m 0644 %s %s",
		shellQuote(parentDir), shellQuote(tempPath), shellQuote(targetPath))
	return runInteractiveCommand("su", "-s", "/bin/bash", "-", "root", "-c", command)
}

func runInteractiveCommand(name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
