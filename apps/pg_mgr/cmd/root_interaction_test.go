package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"pg_mgr/internal/interaction"
)

func TestRootExposesStableInteractionFlags(t *testing.T) {
	wants := []string{"non-interactive", "yes", "output", "lang", "color", "quiet", "verbose"}
	for _, name := range wants {
		if RootCmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("missing persistent flag --%s", name)
		}
	}
}

func TestRenderRootErrorUsesConfiguredModeAndExitCategory(t *testing.T) {
	old := UI
	t.Cleanup(func() { UI = old })
	UI.Output = "json"
	var stdout, stderr bytes.Buffer
	err := interaction.NewError(interaction.CodeTargetNotFound, "instance missing", interaction.ExitTarget)
	code := renderRootError(&stdout, &stderr, err)
	if code != 4 {
		t.Fatalf("exit code = %d, want 4", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `"code":"target_not_found"`) {
		t.Fatalf("stderr = %q", stderr.String())
	}

	UI.Output = "table"
	stderr.Reset()
	if code := renderRootError(&stdout, &stderr, errors.New("boom")); code != 1 {
		t.Fatalf("uncategorized exit code = %d, want 1", code)
	}
	if stderr.String() != "boom\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCobraUsageErrorsMapToExitTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	for _, err := range []error{errors.New("unknown command \"wat\""), errors.New("requires exactly 1 arg(s)")} {
		stderr.Reset()
		if code := renderRootError(&stdout, &stderr, err); code != 2 {
			t.Fatalf("renderRootError(%q) = %d, want 2", err, code)
		}
	}
}

func TestArchiveIsAResourceGroup(t *testing.T) {
	if archiveCmd.Run != nil || archiveCmd.RunE != nil {
		t.Fatal("archive without a subcommand must display help, not run an implicit status action")
	}
}

func TestRequireExplicitIdentityInAutomation(t *testing.T) {
	instance, password := "", ""
	err := requireExplicitIdentity(true, false, &instance, password)
	appErr, ok := err.(*interaction.Error)
	if !ok || appErr.Code != interaction.CodeMissingInput {
		t.Fatalf("error = %#v, want missing_input", err)
	}
	if got := appErr.Details["missing_flags"]; got == nil {
		t.Fatalf("missing_flags absent: %#v", appErr.Details)
	}

	instance = ""
	err = requireExplicitIdentity(true, true, &instance, "from-safe-source")
	if err != nil {
		t.Fatal(err)
	}
	if instance != "default" {
		t.Fatalf("legacy instance = %q, want default", instance)
	}
}

func TestNonInteractiveRemovalRequiresTargetAndApproval(t *testing.T) {
	oldConfig, oldUI := Config, UI
	t.Cleanup(func() { Config, UI = oldConfig, oldUI })
	Config.Silent = true
	Config.InstanceName = ""
	UI = InteractionOptions{NonInteractive: true}
	if err := prepareRemoval(uninstallCmd); err == nil || interaction.ExitCode(err) != 2 {
		t.Fatalf("missing target error = %v", err)
	}
	Config.InstanceName = "sales"
	if err := prepareRemoval(uninstallCmd); err == nil || interaction.ExitCode(err) != 2 {
		t.Fatalf("missing approval error = %v", err)
	}
	UI.Yes = true
	if err := prepareRemoval(uninstallCmd); err != nil {
		t.Fatalf("complete removal invocation: %v", err)
	}
}
