package cmd

import (
	"fmt"
	"os"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

func init() {
	config.PrivilegedWriteFunc = promptPrivilegedConfigWrite
}

// promptPrivilegedConfigWrite intentionally does not elevate in-process. The
// root renderer presents this error and a redacted command the operator can
// inspect and run in a fresh privileged invocation.
func promptPrivilegedConfigWrite(targetPath string, _ []byte) error {
	if utils.IsRoot() {
		return fmt.Errorf("%s", i18n.T("err_permission_write", targetPath))
	}
	retry := interaction.RetryCommand("sudo", os.Args)
	return interaction.NewError(
		interaction.CodePermissionDenied,
		i18n.T("permission_config_write", targetPath),
		interaction.ExitPermission,
	).
		WithDetail("required_identity", "root").
		WithDetail("target", targetPath).
		WithRemediation(i18n.T("retry_with") + "\n  " + retry)
}
