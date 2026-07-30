package dashboard

import (
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// DefaultInterval is the periodic reload cadence of the dashboard.
const DefaultInterval = 2 * time.Second

// Run starts the dashboard TUI on the given input and output, blocking until
// the user quits or the program fails. The alternate screen is used and the
// terminal state is restored by bubbletea on every exit path, including
// Ctrl-C and rendering errors.
func Run(loader func() (Snapshot, error), interval time.Duration, in io.Reader, out io.Writer) error {
	model := NewModel(loader, time.Now, interval)
	program := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen())
	_, err := program.Run()
	return err
}
