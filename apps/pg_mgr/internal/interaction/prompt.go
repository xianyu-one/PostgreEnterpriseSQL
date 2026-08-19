package interaction

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"pg_mgr/internal/i18n"
)

type Prompt struct {
	reader *bufio.Reader
	writer io.Writer
}

func NewPrompt(reader io.Reader, writer io.Writer) *Prompt {
	return &Prompt{reader: bufio.NewReader(reader), writer: writer}
}

// Menu renders a closed choice. It returns a zero-based item index; 0 cancels.
func (p *Prompt) Menu(label string, items []string, defaultIndex int) (int, error) {
	if len(items) == 0 {
		return -1, NewError(CodeInvalidInput, i18n.T("err_empty_menu"), ExitUsage)
	}
	if defaultIndex < 0 || defaultIndex >= len(items) {
		defaultIndex = 0
	}
	for {
		fmt.Fprintln(p.writer, label)
		for i, item := range items {
			fmt.Fprintf(p.writer, "  %d. %s\n", i+1, item)
		}
		fmt.Fprintf(p.writer, "  0. %s\n", i18n.T("menu_cancel"))
		fmt.Fprint(p.writer, i18n.T("menu_choose", defaultIndex+1))
		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return -1, err
		}
		if errors.Is(err, io.EOF) && line == "" {
			return -1, ErrCancelled
		}
		value := strings.TrimSpace(line)
		if value == "" {
			return defaultIndex, nil
		}
		choice, convErr := strconv.Atoi(value)
		if convErr == nil && choice == 0 {
			return -1, ErrCancelled
		}
		if convErr == nil && choice >= 1 && choice <= len(items) {
			return choice - 1, nil
		}
		fmt.Fprintln(p.writer, i18n.T("menu_valid_range", len(items)))
	}
}
