package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var (
	selfUpdateBinary string
	selfUpdateTarget string
)

var selfUpdateCmd = &cobra.Command{
	Use:     "self-update",
	Aliases: []string{"update"},
	Short:   i18n.T("self_update_desc"),
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return runSelfUpdate() },
}

func init() {
	selfUpdateCmd.Flags().StringVarP(&selfUpdateBinary, "binary", "b", "", i18n.T("flag_update_binary"))
	selfUpdateCmd.Flags().StringVar(&selfUpdateTarget, "target", "", i18n.T("flag_update_target"))
	RootCmd.AddCommand(selfUpdateCmd)
}

func runSelfUpdate() error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	if selfUpdateBinary == "" {
		if UI.NonInteractive {
			return interaction.MissingFlags("--binary")
		}
		selfUpdateBinary = utils.PromptPath(i18n.T("prompt_update_binary"), "")
		if selfUpdateBinary == "" {
			return interaction.MissingFlags("--binary")
		}
	}

	candidate, err := filepath.Abs(selfUpdateBinary)
	if err != nil {
		return interaction.NewError(interaction.CodeInvalidInput, err.Error(), interaction.ExitUsage).WithCause(err)
	}
	target := selfUpdateTarget
	if target == "" {
		target, err = os.Executable()
		if err != nil {
			return err
		}
	} else {
		target, err = filepath.Abs(target)
		if err != nil {
			return interaction.NewError(interaction.CodeInvalidInput, err.Error(), interaction.ExitUsage).WithCause(err)
		}
	}
	if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil {
		target = resolved
	}
	if samePath(candidate, target) {
		return interaction.NewError(interaction.CodeResourceConflict, i18n.T("err_update_same_binary"), interaction.ExitTarget)
	}

	candidateVersion, err := validatePgMgrBinary(candidate)
	if err != nil {
		return err
	}
	daemonActive := systemDaemonActive()

	if UI.NonInteractive && !UI.Yes {
		return interaction.MissingFlags("--yes")
	}
	if !UI.NonInteractive {
		fmt.Fprintln(os.Stderr, i18n.T("self_update_review"))
		fmt.Fprintln(os.Stderr, i18n.T("self_update_current", Version))
		fmt.Fprintln(os.Stderr, i18n.T("self_update_candidate", candidateVersion))
		fmt.Fprintln(os.Stderr, i18n.T("self_update_target", target))
		fmt.Fprintln(os.Stderr, i18n.T("self_update_daemon", localizedYesNo(daemonActive)))
		choice, promptErr := interaction.NewPrompt(os.Stdin, os.Stderr).Menu(
			i18n.T("self_update_confirm"),
			[]string{i18n.T("option_yes"), i18n.T("option_no")},
			1,
		)
		if promptErr != nil {
			return promptErr
		}
		if choice != 0 {
			return interaction.ErrCancelled
		}
	}

	if daemonActive {
		if err := utils.RunCmd("systemctl", "stop", "pg_mgr.service"); err != nil {
			return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_update_stop_daemon", err), interaction.ExitExecution).WithCause(err)
		}
	}
	daemonStopped := daemonActive
	defer func() {
		if daemonStopped {
			_ = utils.RunCmd("systemctl", "start", "pg_mgr.service")
		}
	}()

	restart := func() error {
		if !daemonActive {
			return nil
		}
		err := utils.RunCmd("systemctl", "start", "pg_mgr.service")
		if err == nil {
			daemonStopped = false
		}
		return err
	}
	if err := replaceExecutable(candidate, target, restart); err != nil {
		return err
	}

	result := map[string]any{
		"status":           "updated",
		"version":          candidateVersion,
		"executable":       target,
		"daemon_restarted": daemonActive,
	}
	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(result)
	}
	if !UI.Quiet {
		fmt.Println(i18n.T("self_update_success", candidateVersion, target))
	}
	return nil
}

func localizedYesNo(value bool) string {
	if value {
		return i18n.T("option_yes")
	}
	return i18n.T("option_no")
}

func samePath(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func validatePgMgrBinary(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_update_binary", err), interaction.ExitUsage).WithCause(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_update_not_executable", path), interaction.ExitUsage)
	}
	out, err := exec.Command(path, "--version").CombinedOutput()
	versionOutput := strings.TrimSpace(string(out))
	const versionPrefix = "pg_mgr version"
	if err != nil || !strings.HasPrefix(strings.ToLower(versionOutput), versionPrefix) {
		if err == nil {
			err = fmt.Errorf("unexpected version output: %s", versionOutput)
		}
		return "", interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_update_invalid_binary", err), interaction.ExitUsage).WithCause(err)
	}
	version := strings.TrimSpace(versionOutput[len(versionPrefix):])
	if version == "" {
		err := fmt.Errorf("empty version in output: %s", versionOutput)
		return "", interaction.NewError(interaction.CodeInvalidInput, i18n.T("err_update_invalid_binary", err), interaction.ExitUsage).WithCause(err)
	}
	return version, nil
}

func systemDaemonActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "pg_mgr.service").Run() == nil
}

func replaceExecutable(candidate, target string, restart func() error) error {
	targetInfo, err := os.Stat(target)
	if err != nil {
		return err
	}

	source, err := os.Open(candidate)
	if err != nil {
		return err
	}
	defer source.Close()

	temporary, err := os.CreateTemp(filepath.Dir(target), ".pg_mgr-update-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanupTemporary := true
	defer func() {
		_ = temporary.Close()
		if cleanupTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if _, err := io.Copy(temporary, source); err != nil {
		return err
	}
	if err := temporary.Chmod(targetInfo.Mode().Perm()); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if _, err := validatePgMgrBinary(temporaryPath); err != nil {
		return err
	}

	backupPath := temporaryPath + ".previous"
	if err := os.Rename(target, backupPath); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		_ = os.Rename(backupPath, target)
		return err
	}
	cleanupTemporary = false

	if err := restart(); err != nil {
		failedPath := temporaryPath + ".failed"
		_ = os.Rename(target, failedPath)
		restoreErr := os.Rename(backupPath, target)
		restartOldErr := restart()
		_ = os.Remove(failedPath)
		if restoreErr != nil {
			return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_update_rollback", err, restoreErr), interaction.ExitExecution).WithCause(err)
		}
		if restartOldErr != nil {
			return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_update_restart_rollback", err, restartOldErr), interaction.ExitExecution).WithCause(err)
		}
		return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_update_restarted_old", err), interaction.ExitExecution).WithCause(err)
	}

	if err := os.Remove(backupPath); err != nil {
		return interaction.NewError(interaction.CodeExecutionFailed, i18n.T("err_update_cleanup", err), interaction.ExitExecution).WithCause(err)
	}
	return syncDirectory(filepath.Dir(target))
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
