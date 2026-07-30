//go:build unix

package cli

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// ptyDashboard drives "git kura dashboard" on a pseudo terminal.
type ptyDashboard struct {
	t      *testing.T
	master *os.File
	cmd    *exec.Cmd
	mu     sync.Mutex
	output strings.Builder
	done   chan struct{}
}

// startDashboardOnPty launches "git kura dashboard" inside repo on a pseudo
// terminal and starts collecting all terminal output.
func startDashboardOnPty(t *testing.T, c *testCLI, repo string) *ptyDashboard {
	t.Helper()

	cmd := exec.Command("git", "kura", "dashboard")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "PATH="+c.envPath, "TERM=xterm-256color")

	master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("start dashboard on pty: %v", err)
	}

	d := &ptyDashboard{t: t, master: master, cmd: cmd, done: make(chan struct{})}
	go func() {
		defer close(d.done)
		buf := make([]byte, 4096)
		for {
			n, readErr := master.Read(buf)
			if n > 0 {
				d.mu.Lock()
				d.output.Write(buf[:n])
				d.mu.Unlock()
			}
			if readErr != nil {
				// EIO is the normal end-of-stream on Linux when the child exits.
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = master.Close()
	})
	return d
}

func (d *ptyDashboard) snapshot() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.output.String()
}

// waitFor blocks until the collected terminal output contains substr, so key
// presses are never sent before the TUI is up.
func (d *ptyDashboard) waitFor(substr string) {
	d.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(d.snapshot(), substr) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	d.t.Fatalf("dashboard output never contained %q:\n%q", substr, d.snapshot())
}

func (d *ptyDashboard) send(s string) {
	d.t.Helper()
	if _, err := d.master.WriteString(s); err != nil {
		d.t.Fatalf("send %q: %v", s, err)
	}
}

// wait blocks until the child exits and returns all collected output.
func (d *ptyDashboard) wait() (string, error) {
	d.t.Helper()
	waitDone := make(chan error, 1)
	go func() { waitDone <- d.cmd.Wait() }()
	select {
	case err := <-waitDone:
		<-d.done
		return d.snapshot(), err
	case <-time.After(30 * time.Second):
		_ = d.cmd.Process.Kill()
		d.t.Fatalf("dashboard did not exit\noutput:\n%q", d.snapshot())
		return "", nil
	}
}

func TestDashboardPtyQuitOnQRestoresTerminal(t *testing.T) {
	c := newTestCLI(t)
	repo := c.initRepo(t)

	d := startDashboardOnPty(t, c, repo)
	d.waitFor("git-kura dashboard")
	d.send("q")

	output, err := d.wait()
	if err != nil {
		t.Fatalf("dashboard exited with error: %v\noutput:\n%s", err, output)
	}
	if !strings.Contains(output, "\x1b[?1049h") {
		t.Fatalf("output missing alternate screen enter sequence:\n%q", output)
	}
	if !strings.Contains(output, "\x1b[?1049l") {
		t.Fatalf("output missing alternate screen leave (terminal restore) sequence:\n%q", output)
	}
}

func TestDashboardPtyQuitOnCtrlCRestoresTerminal(t *testing.T) {
	c := newTestCLI(t)
	repo := c.initRepo(t)

	d := startDashboardOnPty(t, c, repo)
	d.waitFor("git-kura dashboard")
	d.send("\x03")

	output, err := d.wait()
	if err != nil {
		t.Fatalf("dashboard exited with error on ctrl+c: %v\noutput:\n%s", err, output)
	}
	if !strings.Contains(output, "\x1b[?1049l") {
		t.Fatalf("output missing terminal restore sequence:\n%q", output)
	}
}

// TestDashboardNonInteractiveBinaryFails runs the real binary with pipes for
// stdio: it must fail without writing any escape sequence to stdout.
func TestDashboardNonInteractiveBinaryFails(t *testing.T) {
	c := newTestCLI(t)
	repo := c.initRepo(t)

	res := c.gitKura(repo, "dashboard")
	requireNonZeroExitCode(t, res)
	requireStderrContains(t, res, "interactive terminal")
	requireEmptyStdout(t, res)
}
