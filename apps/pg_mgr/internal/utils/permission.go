package utils

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
)

var (
	// RootCheckOverride can be set in tests to override root check behavior
	RootCheckOverride *bool
)

// IsRoot checks if the current user is root (EUID == 0).
func IsRoot() bool {
	if RootCheckOverride != nil {
		return *RootCheckOverride
	}
	return os.Geteuid() == 0
}

// GetCurrentOSUser returns the current OS username.
func GetCurrentOSUser() (string, error) {
	u, err := user.Current()
	if err == nil {
		return u.Username, nil
	}
	euid := os.Geteuid()
	uObj, err2 := user.LookupId(strconv.Itoa(euid))
	if err2 == nil {
		return uObj.Username, nil
	}
	return "", err
}

// IsRootOrUser checks if current user is root OR targetUser.
func IsRootOrUser(targetUser string) bool {
	if IsRoot() {
		return true
	}
	if targetUser == "" {
		return false
	}
	currUser, err := GetCurrentOSUser()
	if err != nil {
		return false
	}
	return currUser == targetUser
}

// EnsureRoot checks that the current user is root.
func EnsureRoot() error {
	return CheckRoot()
}

func CheckRoot() error {
	if IsRoot() {
		return nil
	}
	return interaction.NewError(interaction.CodePermissionDenied, i18n.T("req_root"), interaction.ExitPermission).
		WithDetail("required_identity", "root").
		WithRemediation(i18n.T("retry_with") + "\n  " + interaction.RetryCommand("sudo", os.Args))
}

func CheckUserPermission(targetUser string) error {
	if IsRootOrUser(targetUser) {
		return nil
	}
	return interaction.NewError(
		interaction.CodePermissionDenied,
		i18n.T("req_root_or_user", targetUser),
		interaction.ExitPermission,
	).WithDetail("allowed_identities", []string{"root", targetUser}).
		WithRemediation(i18n.T("retry_with") + "\n  " + interaction.RetryCommand("sudo", os.Args))
}

func CheckInstancePermission(instanceName string) error {
	if IsRoot() {
		return nil
	}
	targetUser := "postgres"
	if meta, ok := config.Global.Instances[instanceName]; ok {
		targetUser = meta.User
	}
	return CheckUserPermission(targetUser)
}

// EnsureInstancePermission checks if current user is root OR the instance daemon user.
func EnsureInstancePermission(instanceName string) error {
	return CheckInstancePermission(instanceName)
}

// EnsureUserPermission checks if current user is root OR targetUser.
func EnsureUserPermission(targetUser string) error {
	return CheckUserPermission(targetUser)
}

// EnsurePkgPermissions sets minimal accessible permissions on a software version package directory (0755 for dirs/executables, 0644 for files) without altering file ownership.
func EnsurePkgPermissions(versionPath string) error {
	if versionPath == "" {
		return nil
	}
	cleanPath := filepath.Clean(versionPath)
	fi, err := os.Stat(cleanPath)
	if err != nil {
		return err
	}

	// Ensure parent directories down to versionPath are accessible (0755)
	curr := cleanPath
	for curr != "/" && curr != "." {
		if info, err := os.Stat(curr); err == nil && info.IsDir() {
			mode := info.Mode().Perm()
			if mode&0755 != 0755 {
				_ = os.Chmod(curr, 0755)
			}
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	if !fi.IsDir() {
		return nil
	}

	return filepath.Walk(cleanPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		mode := info.Mode().Perm()
		if info.IsDir() {
			if mode != 0755 {
				_ = os.Chmod(path, 0755)
			}
		} else {
			isExec := (mode&0111 != 0) || strings.Contains(path, "/bin/") || strings.HasSuffix(path, ".so") || strings.Contains(path, ".so.")
			if isExec {
				if mode != 0755 {
					_ = os.Chmod(path, 0755)
				}
			} else {
				if mode != 0644 {
					_ = os.Chmod(path, 0644)
				}
			}
		}
		return nil
	})
}

// DetectDirOwner attempts to find the OS username of the owner of dirPath.
func DetectDirOwner(dirPath string) string {
	cleanPath := filepath.Clean(dirPath)
	fi, err := os.Stat(cleanPath)
	if err != nil {
		return ""
	}
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(stat.Uid), 10))
	if err != nil {
		return ""
	}
	return u.Username
}

// ChownDirRecursively recursively changes ownership of dirPath to uid:gid.
func ChownDirRecursively(dirPath string, uid, gid int) error {
	if dirPath == "" {
		return nil
	}
	cleanPath := filepath.Clean(dirPath)
	fi, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return os.Chown(cleanPath, uid, gid)
	}
	return filepath.Walk(cleanPath, func(path string, info os.FileInfo, err error) error {
		if err == nil {
			_ = os.Chown(path, uid, gid)
		}
		return nil
	})
}

// ChangeInstanceOwnership changes ownership of all directories associated with an instance (DataDir, BackupDir, ArchiveDir, Log dirs) to newOSUser.
func ChangeInstanceOwnership(instanceName string, meta config.InstanceMeta, newDataDir string, newOSUser string) error {
	u, err := user.Lookup(newOSUser)
	if err != nil {
		return fmt.Errorf("user %s not found: %v", newOSUser, err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// Data directory
	dataDir := meta.DataDir
	if newDataDir != "" {
		dataDir = newDataDir
	}
	if err := ChownDirRecursively(dataDir, uid, gid); err != nil {
		return err
	}

	// Configured BackupDir and log paths
	if meta.Pgrman != nil {
		if meta.Pgrman.BackupDir != "" {
			_ = ChownDirRecursively(meta.Pgrman.BackupDir, uid, gid)
		}
		if meta.Pgrman.SrvLogPath != "" {
			_ = ChownDirRecursively(meta.Pgrman.SrvLogPath, uid, gid)
		}
		if meta.Pgrman.ArcLogPath != "" {
			_ = ChownDirRecursively(meta.Pgrman.ArcLogPath, uid, gid)
		}
	}

	// Default backup directory if present
	baseDir := config.Global.BaseDir
	defaultBackupDir := filepath.Join(baseDir, fmt.Sprintf("backup_%s", instanceName))
	if _, err := os.Stat(defaultBackupDir); err == nil {
		_ = ChownDirRecursively(defaultBackupDir, uid, gid)
	}

	// WAL Archive directory configured in postgresql.conf
	confPath := filepath.Join(dataDir, "postgresql.conf")
	if arcDir := GetPgMgrArchiveDir(confPath); arcDir != "" {
		_ = ChownDirRecursively(arcDir, uid, gid)
	}

	return nil
}
