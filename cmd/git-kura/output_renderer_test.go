package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

type sampleData struct {
	Value string `json:"value"`
}

func (d sampleData) RenderHuman(w io.Writer) error {
	_, err := fmt.Fprintln(w, d.Value)
	return err
}

type failWriter struct{}

func (failWriter) Write(p []byte) (int, error) { return 0, fmt.Errorf("write failed") }

func TestJSONRendererRendersSchemaValidSuccessEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := selectRenderer(renderJSON)
	if err := r.RenderResult(&stdout, &stderr, Result{
		Command: commandGet,
		Data:    sampleData{Value: "hello"},
	}); err != nil {
		t.Fatalf("RenderResult: %v", err)
	}

	requireConformsToEnvelopeSchema(t, stdout.String())
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	env := decodeEnvelope(t, stdout.String())
	if env["ok"] != true {
		t.Fatalf("ok = %v, want true", env["ok"])
	}
	if env["command"] != "get" {
		t.Fatalf("command = %v, want get", env["command"])
	}
	if _, hasError := env["error"]; hasError {
		t.Fatal("success envelope must not contain error")
	}
	if warnings, ok := env["warnings"].([]any); !ok || len(warnings) != 0 {
		t.Fatalf("warnings = %v, want empty array", env["warnings"])
	}
}

func TestJSONRendererRendersSchemaValidErrorEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := selectRenderer(renderJSON)
	if err := r.RenderError(&stdout, &stderr, &CommandError{
		Command:  commandSealClaim,
		Code:     "seal-conflict",
		Message:  "path is already claimed",
		ExitCode: exitSealConflict,
	}); err != nil {
		t.Fatalf("RenderError: %v", err)
	}

	requireConformsToEnvelopeSchema(t, stdout.String())
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	env := decodeEnvelope(t, stdout.String())
	if env["ok"] != false {
		t.Fatalf("ok = %v, want false", env["ok"])
	}
	if _, hasData := env["data"]; hasData {
		t.Fatal("error envelope must not contain data")
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %v, want object", env["error"])
	}
	if errObj["code"] != "seal-conflict" {
		t.Fatalf("error.code = %v, want seal-conflict", errObj["code"])
	}
}

func TestJSONRendererCarriesWarnings(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := selectRenderer(renderJSON)
	if err := r.RenderResult(&stdout, &stderr, Result{
		Command:  commandOpen,
		Data:     sampleData{Value: "x"},
		Warnings: []Warning{{Code: "stale-metadata", Message: "metadata is stale"}},
	}); err != nil {
		t.Fatalf("RenderResult: %v", err)
	}
	requireConformsToEnvelopeSchema(t, stdout.String())
	env := decodeEnvelope(t, stdout.String())
	warnings, ok := env["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one entry", env["warnings"])
	}
}

func TestHumanRendererWritesToProvidedWriters(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := selectRenderer(renderHuman)
	if err := r.RenderResult(&stdout, &stderr, Result{
		Command:  commandGet,
		Data:     sampleData{Value: "the-path"},
		Warnings: []Warning{{Code: "stale-metadata", Message: "metadata is stale"}},
	}); err != nil {
		t.Fatalf("RenderResult: %v", err)
	}

	if got := strings.TrimSpace(stdout.String()); got != "the-path" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "the-path")
	}
	if !strings.Contains(stderr.String(), "metadata is stale") {
		t.Fatalf("stderr = %q, want it to contain the warning", stderr.String())
	}
	if strings.Contains(stdout.String(), "\"ok\"") {
		t.Fatalf("human stdout must not be an envelope: %q", stdout.String())
	}
}

func TestHumanRendererWritesErrorToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := selectRenderer(renderHuman)
	if err := r.RenderError(&stdout, &stderr, &CommandError{
		Command:  commandClose,
		Code:     "unsafe-refused",
		Message:  "refusing to remove a dirty worktree",
		ExitCode: exitUnsafeRefused,
	}); err != nil {
		t.Fatalf("RenderError: %v", err)
	}

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty for human error", stdout.String())
	}
	if !strings.Contains(stderr.String(), "refusing to remove a dirty worktree") {
		t.Fatalf("stderr = %q, want it to contain the message", stderr.String())
	}
}

func TestHumanRendererSkipsNonRenderableData(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := selectRenderer(renderHuman)
	if err := r.RenderResult(&stdout, &stderr, Result{
		Command:  commandLs,
		Data:     struct{ N int }{N: 1},
		Warnings: []Warning{{Code: "heads-up", Message: "note"}},
	}); err != nil {
		t.Fatalf("RenderResult: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty for non-renderable data", stdout.String())
	}
	if !strings.Contains(stderr.String(), "note") {
		t.Fatalf("stderr = %q, want it to contain the warning", stderr.String())
	}
}

func TestNormalizeWarningsNeverReturnsNil(t *testing.T) {
	if got := normalizeWarnings(nil); got == nil || len(got) != 0 {
		t.Fatalf("normalizeWarnings(nil) = %v, want empty non-nil slice", got)
	}
	in := []Warning{{Code: "x", Message: "y"}}
	if got := normalizeWarnings(in); len(got) != 1 || got[0] != in[0] {
		t.Fatalf("normalizeWarnings(%v) = %v, want it unchanged", in, got)
	}
}

func TestCommandErrorErrorReturnsMessage(t *testing.T) {
	e := &CommandError{Command: commandClose, Code: "unsafe-refused", Message: "boom"}
	if e.Error() != "boom" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "boom")
	}
}

func TestRenderersPropagateWriteErrors(t *testing.T) {
	json := selectRenderer(renderJSON)
	human := selectRenderer(renderHuman)
	var discard bytes.Buffer

	t.Run("json result write failure", func(t *testing.T) {
		err := json.RenderResult(failWriter{}, &discard, Result{Command: commandGet, Data: sampleData{Value: "x"}})
		if err == nil {
			t.Fatal("RenderResult on failing stdout = nil, want error")
		}
	})
	t.Run("json error write failure", func(t *testing.T) {
		err := json.RenderError(failWriter{}, &discard, &CommandError{Command: commandGet, Code: "x", Message: "y"})
		if err == nil {
			t.Fatal("RenderError on failing stdout = nil, want error")
		}
	})
	t.Run("human data write failure", func(t *testing.T) {
		err := human.RenderResult(failWriter{}, &discard, Result{Command: commandGet, Data: sampleData{Value: "x"}})
		if err == nil {
			t.Fatal("RenderResult on failing stdout = nil, want error")
		}
	})
	t.Run("human warning write failure", func(t *testing.T) {
		err := human.RenderResult(&discard, failWriter{}, Result{
			Command:  commandGet,
			Warnings: []Warning{{Code: "x", Message: "y"}},
		})
		if err == nil {
			t.Fatal("RenderResult on failing stderr = nil, want error")
		}
	})
	t.Run("human error warning write failure", func(t *testing.T) {
		err := human.RenderError(&discard, failWriter{}, &CommandError{
			Command:  commandGet,
			Code:     "x",
			Message:  "y",
			Warnings: []Warning{{Code: "w", Message: "z"}},
		})
		if err == nil {
			t.Fatal("RenderError on failing stderr = nil, want error")
		}
	})
}
