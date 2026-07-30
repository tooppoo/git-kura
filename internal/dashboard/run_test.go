package dashboard

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestRunQuitsOnQ drives the full bubbletea program headless: input comes
// from an in-memory reader and output goes to a buffer, so no terminal is
// involved.
func TestRunQuitsOnQ(t *testing.T) {
	loader := func() (Snapshot, error) { return testSnapshot(), nil }
	in := strings.NewReader("q")
	var out bytes.Buffer

	done := make(chan error, 1)
	go func() { done <- Run(loader, time.Hour, in, &out) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("Run did not quit on q")
	}

	if !strings.Contains(out.String(), "git-kura dashboard") {
		t.Fatalf("output missing dashboard header:\n%q", out.String())
	}
}
