package interaction

import (
	"bytes"
	"strings"
	"testing"
)

func TestRuntimeInteractionPolicy(t *testing.T) {
	tests := []struct {
		name string
		rt   Runtime
		want bool
	}{
		{name: "terminal table mode", rt: Runtime{StdinTerminal: true, StderrTerminal: true, Output: OutputTable}, want: true},
		{name: "explicit non interactive", rt: Runtime{StdinTerminal: true, StderrTerminal: true, Output: OutputTable, NonInteractive: true}},
		{name: "json", rt: Runtime{StdinTerminal: true, StderrTerminal: true, Output: OutputJSON}},
		{name: "piped stdin", rt: Runtime{StderrTerminal: true, Output: OutputTable}},
		{name: "redirected stderr", rt: Runtime{StdinTerminal: true, Output: OutputTable}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rt.Interactive(); got != tt.want {
				t.Fatalf("Interactive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMenuRetriesAndAllowsCancellation(t *testing.T) {
	var stderr bytes.Buffer
	prompt := NewPrompt(strings.NewReader("x\n4\n2\n"), &stderr)
	choice, err := prompt.Menu("Choose", []string{"one", "two"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if choice != 1 {
		t.Fatalf("choice = %d, want 1", choice)
	}
	if got := strings.Count(stderr.String(), "valid selection is 0-2"); got != 2 {
		t.Fatalf("validation count = %d, output = %q", got, stderr.String())
	}

	prompt = NewPrompt(strings.NewReader("0\n"), &stderr)
	if _, err := prompt.Menu("Choose", []string{"one"}, 0); err != ErrCancelled {
		t.Fatalf("Menu() error = %v, want ErrCancelled", err)
	}
}

func TestRendererWritesJSONErrorsOnlyToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := NewRenderer(&stdout, &stderr, OutputJSON, false)
	err := NewError(CodePermissionDenied, "Root privileges are required.", ExitPermission).
		WithDetail("required_identity", "root")
	if renderErr := r.Error(err); renderErr != nil {
		t.Fatal(renderErr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	want := `{"code":"permission_denied","message":"Root privileges are required.","details":{"required_identity":"root"}}` + "\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
	if ExitCode(err) != 3 {
		t.Fatalf("ExitCode() = %d, want 3", ExitCode(err))
	}
}

func TestRendererRedactsSecrets(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := NewRenderer(&stdout, &stderr, OutputTable, false)
	r.Review("Create", []ReviewField{
		{Label: "Instance", Value: "sales"},
		{Label: "Password", Value: "top-secret", Secret: true, Source: "terminal"},
		{Label: "Version", Value: "17.10", Automatic: true},
	})
	got := stderr.String()
	if strings.Contains(got, "top-secret") {
		t.Fatalf("review leaked secret: %q", got)
	}
	for _, want := range []string{"Create", "sales", "set (terminal)", "17.10 (automatic)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("review missing %q: %q", want, got)
		}
	}
}

func TestQuietDoesNotSuppressStructuredResults(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := NewRenderer(&stdout, &stderr, OutputJSON, true)
	if err := r.Success(map[string]string{"status": "ok"}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
