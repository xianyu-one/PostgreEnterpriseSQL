package utils

import (
	"testing"
)

func TestIsRootOrUser(t *testing.T) {
	currUser, err := GetCurrentOSUser()
	if err != nil {
		t.Fatalf("failed to get current OS user: %v", err)
	}

	// 1. Same user should return true
	if !IsRootOrUser(currUser) {
		t.Errorf("expected IsRootOrUser(%s) to return true", currUser)
	}

	// 2. Override root check to simulate root
	overrideTrue := true
	RootCheckOverride = &overrideTrue
	defer func() { RootCheckOverride = nil }()

	if !IsRootOrUser("some_other_user_12345") {
		t.Errorf("expected IsRootOrUser to return true when root override is set")
	}

	// 3. Override root check to simulate non-root
	overrideFalse := false
	RootCheckOverride = &overrideFalse

	if IsRootOrUser("non_existent_user_99999") {
		t.Errorf("expected IsRootOrUser to return false for non-matching user when non-root")
	}
}
