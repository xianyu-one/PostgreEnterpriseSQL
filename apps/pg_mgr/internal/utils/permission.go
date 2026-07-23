package utils

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"github.com/jedib0t/go-pretty/v6/text"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
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
func EnsureRoot() {
	if !IsRoot() {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root")))
		os.Exit(1)
	}
}

// EnsureInstancePermission checks if current user is root OR the instance daemon user.
func EnsureInstancePermission(instanceName string) {
	if IsRoot() {
		return
	}
	meta, ok := config.Global.Instances[instanceName]
	if !ok {
		// If instance is not in registry, root is required or error out
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root_or_user", "postgres")))
		os.Exit(1)
	}
	if !IsRootOrUser(meta.User) {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root_or_user", meta.User)))
		os.Exit(1)
	}
}

// EnsureUserPermission checks if current user is root OR targetUser.
func EnsureUserPermission(targetUser string) {
	if !IsRootOrUser(targetUser) {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root_or_user", targetUser)))
		os.Exit(1)
	}
}
