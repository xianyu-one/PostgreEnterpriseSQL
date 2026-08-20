package interaction

import (
	"fmt"
	"io"

	"pg_mgr/internal/i18n"
)

type StageState string

const (
	StagePending    StageState = "pending"
	StageRunning    StageState = "running"
	StageCompleted  StageState = "completed"
	StageFailed     StageState = "failed"
	StageRolledBack StageState = "rolled_back"
	StageSkipped    StageState = "skipped"
)

type Stage struct {
	Name  string     `json:"name"`
	State StageState `json:"state"`
}

type OperationResult struct {
	Stages   []Stage  `json:"stages"`
	Retained []string `json:"retained,omitempty"`
	Recovery []string `json:"recovery,omitempty"`
}

type Operation struct {
	writer io.Writer
	mode   OutputMode
	result OperationResult
}

func NewOperation(writer io.Writer, mode OutputMode) *Operation {
	return &Operation{writer: writer, mode: mode, result: OperationResult{Stages: make([]Stage, 0)}}
}

func (o *Operation) Run(name string, action func() error) error {
	stage := Stage{Name: name, State: StageRunning}
	index := len(o.result.Stages)
	o.result.Stages = append(o.result.Stages, stage)
	o.line(i18n.T("stage_running"), name)
	if err := action(); err != nil {
		o.result.Stages[index].State = StageFailed
		o.line(i18n.T("stage_failed"), name)
		return err
	}
	o.result.Stages[index].State = StageCompleted
	o.line(i18n.T("stage_completed"), name)
	return nil
}

func (o *Operation) RolledBack(name string) {
	o.result.Stages = append(o.result.Stages, Stage{Name: name, State: StageRolledBack})
	o.line(i18n.T("stage_rolled_back"), name)
}

func (o *Operation) Retain(value string) { o.result.Retained = append(o.result.Retained, value) }

func (o *Operation) RecoverWith(value string) { o.result.Recovery = append(o.result.Recovery, value) }

func (o *Operation) Result() OperationResult { return o.result }

func (o *Operation) line(state, name string) {
	if o.mode != OutputJSON {
		fmt.Fprintf(o.writer, "%s: %s\n", state, name)
	}
}
