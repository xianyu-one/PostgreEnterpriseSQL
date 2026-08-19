package interaction

import "io"

type OutputMode string

const (
	OutputTable OutputMode = "table"
	OutputJSON  OutputMode = "json"
)

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

// Runtime contains invocation-wide interaction policy and stream ownership.
type Runtime struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	StdinTerminal  bool
	StderrTerminal bool
	NonInteractive bool
	Yes            bool
	Quiet          bool
	Verbose        bool
	Output         OutputMode
	Color          ColorMode
	Language       string
}

func (r Runtime) Interactive() bool {
	return !r.NonInteractive && r.Output != OutputJSON && r.StdinTerminal && r.StderrTerminal
}
