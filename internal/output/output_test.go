package output_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	toon "github.com/toon-format/toon-go"
	"github.com/tooppoo/git-kura/internal/output"
)

// sampleData is a minimal HumanRenderable payload used across renderer tests.
type sampleData struct {
	Value string `json:"value"`
}

func (d sampleData) RenderHuman(w io.Writer) error {
	_, err := fmt.Fprintln(w, d.Value)
	return err
}

// failWriter always returns a write error.
type failWriter struct{}

func (failWriter) Write(p []byte) (int, error) { return 0, fmt.Errorf("write failed") }

// requireConformsToEnvelopeSchema asserts that jsonOutput validates against the
// envelope schema.
func requireConformsToEnvelopeSchema(t *testing.T, jsonOutput string) {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(jsonOutput))
	if err != nil {
		t.Fatalf("parse json output: %v\noutput: %s", err, jsonOutput)
	}
	if err := output.Schema.Validate(inst); err != nil {
		t.Fatalf("json output does not conform to envelope schema:\n%v\noutput: %s", err, jsonOutput)
	}
}

func decodeEnvelope(t *testing.T, jsonOutput string) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(jsonOutput), &env); err != nil {
		t.Fatalf("decode envelope: %v\noutput: %s", err, jsonOutput)
	}
	return env
}

// --- Schema tests ---

func TestEnvelopeSchemaAcceptsValidEnvelopes(t *testing.T) {
	cases := map[string]string{
		"success with empty warnings": `{"ok":true,"command":"get","schemaVersion":1,"data":{"value":"x"},"warnings":[]}`,
		"success with warnings":       `{"ok":true,"command":"open","schemaVersion":1,"data":{},"warnings":[{"code":"some-warning","message":"heads up"}]}`,
		"failure with empty warnings": `{"ok":false,"command":"seal.claim","schemaVersion":1,"warnings":[],"error":{"code":"seal-conflict","message":"already claimed"}}`,
		"failure with details":        `{"ok":false,"command":"close","schemaVersion":1,"warnings":[],"error":{"code":"unsafe-refused","message":"refused","details":{"path":"x"}}}`,
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			requireConformsToEnvelopeSchema(t, env)
		})
	}
}

func TestSchemaCommandEnumMatchesAllCommands(t *testing.T) {
	var schemaDoc struct {
		Properties struct {
			Command struct {
				Enum []string `json:"enum"`
			} `json:"command"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(output.SchemaJSON, &schemaDoc); err != nil {
		t.Fatalf("parse envelope schema: %v", err)
	}

	want := make([]string, len(output.AllCommands))
	for i, c := range output.AllCommands {
		want[i] = string(c)
	}
	got := append([]string(nil), schemaDoc.Properties.Command.Enum...)

	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Fatalf("schema command enum %v does not match AllCommands %v", got, want)
	}
}

func TestEncodeEnvelopeSchemaValidationFailure(t *testing.T) {
	_, err := output.EncodeEnvelope(output.Envelope{})
	if err == nil {
		t.Fatal("EncodeEnvelope with empty Envelope: expected schema validation error, got nil")
	}
}

func TestWriteEnvelopePropagatesEncodeError(t *testing.T) {
	var buf strings.Builder
	err := output.WriteEnvelope(&buf, output.Envelope{
		OK:            true,
		Command:       output.CommandGet,
		SchemaVersion: output.SchemaVersion,
		Warnings:      []output.Warning{},
	})
	if err == nil {
		t.Fatal("WriteEnvelope with invalid envelope = nil, want error")
	}
	if buf.Len() != 0 {
		t.Fatalf("buf = %q, want empty on encode failure", buf.String())
	}
}

// --- JSON renderer tests ---

func TestJSONRendererRendersSchemaValidSuccessEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := output.SelectRenderer(output.RenderJSON)
	if err := r.RenderResult(&stdout, &stderr, output.Result{
		Command: output.CommandGet,
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
	r := output.SelectRenderer(output.RenderJSON)
	if err := r.RenderError(&stdout, &stderr, &output.CommandError{
		Command:  output.CommandSealClaim,
		Code:     "seal-conflict",
		Message:  "path is already claimed",
		ExitCode: 6,
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
	r := output.SelectRenderer(output.RenderJSON)
	if err := r.RenderResult(&stdout, &stderr, output.Result{
		Command:  output.CommandOpen,
		Data:     sampleData{Value: "x"},
		Warnings: []output.Warning{{Code: "stale-metadata", Message: "metadata is stale"}},
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

// --- Human renderer tests ---

func TestHumanRendererWritesToProvidedWriters(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := output.SelectRenderer(output.RenderHuman)
	if err := r.RenderResult(&stdout, &stderr, output.Result{
		Command:  output.CommandGet,
		Data:     sampleData{Value: "the-path"},
		Warnings: []output.Warning{{Code: "stale-metadata", Message: "metadata is stale"}},
	}); err != nil {
		t.Fatalf("RenderResult: %v", err)
	}

	if got := strings.TrimSpace(stdout.String()); got != "the-path" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "the-path")
	}
	if !strings.Contains(stderr.String(), "metadata is stale") {
		t.Fatalf("stderr = %q, want it to contain the warning", stderr.String())
	}
}

func TestHumanRendererWritesErrorToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := output.SelectRenderer(output.RenderHuman)
	if err := r.RenderError(&stdout, &stderr, &output.CommandError{
		Command:  output.CommandClose,
		Code:     "unsafe-refused",
		Message:  "refusing to remove a dirty worktree",
		ExitCode: 3,
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
	r := output.SelectRenderer(output.RenderHuman)
	if err := r.RenderResult(&stdout, &stderr, output.Result{
		Command:  output.CommandLs,
		Data:     struct{ N int }{N: 1},
		Warnings: []output.Warning{{Code: "heads-up", Message: "note"}},
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

// --- TOON renderer tests ---

func TestTOONRendererRendersSuccessEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := output.SelectRenderer(output.RenderTOON)
	if err := r.RenderResult(&stdout, &stderr, output.Result{
		Command: output.CommandGet,
		Data:    sampleData{Value: "hello"},
	}); err != nil {
		t.Fatalf("RenderResult: %v", err)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"ok", "command", "schemaVersion", "warnings"} {
		if !strings.Contains(out, want) {
			t.Fatalf("TOON output = %q, want it to contain field %q", out, want)
		}
	}
}

func TestTOONRendererRendersErrorEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := output.SelectRenderer(output.RenderTOON)
	if err := r.RenderError(&stdout, &stderr, &output.CommandError{
		Command:  output.CommandSealClaim,
		Code:     "seal-conflict",
		Message:  "path is already claimed",
		ExitCode: 6,
	}); err != nil {
		t.Fatalf("RenderError: %v", err)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"ok", "command", "schemaVersion", "error", "warnings"} {
		if !strings.Contains(out, want) {
			t.Fatalf("TOON output = %q, want it to contain field %q", out, want)
		}
	}
}

func TestTOONRendererIsDecodable(t *testing.T) {
	var stdout bytes.Buffer
	r := output.SelectRenderer(output.RenderTOON)
	if err := r.RenderResult(&stdout, &bytes.Buffer{}, output.Result{
		Command: output.CommandLs,
		Data:    sampleData{Value: "k1"},
	}); err != nil {
		t.Fatalf("RenderResult: %v", err)
	}

	decoded, err := toon.DecodeString(strings.TrimSpace(stdout.String()))
	if err != nil {
		t.Fatalf("toon.DecodeString: %v", err)
	}
	obj, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("decoded = %T, want map[string]any", decoded)
	}
	for _, key := range []string{"ok", "command", "schemaVersion", "data", "warnings"} {
		if _, exists := obj[key]; !exists {
			t.Fatalf("decoded TOON missing key %q", key)
		}
	}
	if obj["ok"] != true {
		t.Fatalf("ok = %v, want true", obj["ok"])
	}
	if obj["command"] != "ls" {
		t.Fatalf("command = %v, want ls", obj["command"])
	}
}

func TestTOONRendererPropagatesWriteError(t *testing.T) {
	r := output.SelectRenderer(output.RenderTOON)
	err := r.RenderResult(failWriter{}, &bytes.Buffer{}, output.Result{Command: output.CommandGet, Data: sampleData{Value: "x"}})
	if err == nil {
		t.Fatal("RenderResult on failing stdout = nil, want error")
	}
}

// --- Utility tests ---

func TestNormalizeWarningsNeverReturnsNil(t *testing.T) {
	if got := output.NormalizeWarnings(nil); got == nil || len(got) != 0 {
		t.Fatalf("NormalizeWarnings(nil) = %v, want empty non-nil slice", got)
	}
	in := []output.Warning{{Code: "x", Message: "y"}}
	if got := output.NormalizeWarnings(in); len(got) != 1 || got[0] != in[0] {
		t.Fatalf("NormalizeWarnings(%v) = %v, want it unchanged", in, got)
	}
}

func TestCommandErrorErrorReturnsMessage(t *testing.T) {
	e := &output.CommandError{Command: output.CommandClose, Code: "unsafe-refused", Message: "boom"}
	if e.Error() != "boom" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "boom")
	}
}

func TestRenderersPropagateWriteErrors(t *testing.T) {
	jsonR := output.SelectRenderer(output.RenderJSON)
	human := output.SelectRenderer(output.RenderHuman)
	var discard bytes.Buffer

	t.Run("json result write failure", func(t *testing.T) {
		err := jsonR.RenderResult(failWriter{}, &discard, output.Result{Command: output.CommandGet, Data: sampleData{Value: "x"}})
		if err == nil {
			t.Fatal("RenderResult on failing stdout = nil, want error")
		}
	})
	t.Run("json error write failure", func(t *testing.T) {
		err := jsonR.RenderError(failWriter{}, &discard, &output.CommandError{Command: output.CommandGet, Code: "x", Message: "y"})
		if err == nil {
			t.Fatal("RenderError on failing stdout = nil, want error")
		}
	})
	t.Run("human data write failure", func(t *testing.T) {
		err := human.RenderResult(failWriter{}, &discard, output.Result{Command: output.CommandGet, Data: sampleData{Value: "x"}})
		if err == nil {
			t.Fatal("RenderResult on failing stdout = nil, want error")
		}
	})
	t.Run("human warning write failure", func(t *testing.T) {
		err := human.RenderResult(&discard, failWriter{}, output.Result{
			Command:  output.CommandGet,
			Warnings: []output.Warning{{Code: "x", Message: "y"}},
		})
		if err == nil {
			t.Fatal("RenderResult on failing stderr = nil, want error")
		}
	})
	t.Run("human error warning write failure", func(t *testing.T) {
		err := human.RenderError(&discard, failWriter{}, &output.CommandError{
			Command:  output.CommandGet,
			Code:     "x",
			Message:  "y",
			Warnings: []output.Warning{{Code: "w", Message: "z"}},
		})
		if err == nil {
			t.Fatal("RenderError on failing stderr = nil, want error")
		}
	})
}
