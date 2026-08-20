package interaction

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestOperationTracksStagesAndRecovery(t *testing.T) {
	var stderr bytes.Buffer
	op := NewOperation(&stderr, OutputTable)
	if err := op.Run("create directory", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("permission denied")
	if err := op.Run("create service", func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("Run() = %v", err)
	}
	op.RolledBack("remove service file")
	op.Retain("/data/sales")
	op.RecoverWith("fix permissions and retry")
	result := op.Result()
	if result.Stages[0].State != StageCompleted || result.Stages[1].State != StageFailed || result.Stages[2].State != StageRolledBack {
		t.Fatalf("stages = %#v", result.Stages)
	}
	for _, want := range []string{"running: create directory", "completed: create directory", "running: create service", "failed: create service", "rolled back: remove service file"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("output missing %q: %q", want, stderr.String())
		}
	}
}

func TestJSONOperationDoesNotEmitProgress(t *testing.T) {
	var stderr bytes.Buffer
	op := NewOperation(&stderr, OutputJSON)
	_ = op.Run("stage", func() error { return nil })
	if stderr.Len() != 0 {
		t.Fatalf("JSON progress = %q, want empty", stderr.String())
	}
}
