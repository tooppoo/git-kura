package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// requireConformsToEnvelopeSchema is the common test helper for validating that
// a JSON string conforms to schema/envelope.schema.json. Command-specific
// output tests in follow-up issues reuse it to assert envelope conformance.
func requireConformsToEnvelopeSchema(t *testing.T, jsonOutput string) {
	t.Helper()

	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(jsonOutput))
	if err != nil {
		t.Fatalf("parse json output: %v\noutput: %s", err, jsonOutput)
	}
	if err := envelopeSchema.Validate(inst); err != nil {
		t.Fatalf("json output does not conform to envelope schema:\n%v\noutput: %s", err, jsonOutput)
	}
}

func requireViolatesEnvelopeSchema(t *testing.T, jsonOutput string) {
	t.Helper()

	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(jsonOutput))
	if err != nil {
		// Unparseable JSON is, for our purposes, also non-conforming.
		return
	}
	if err := envelopeSchema.Validate(inst); err == nil {
		t.Fatalf("json output unexpectedly conforms to envelope schema:\n%s", jsonOutput)
	}
}

// sampleData is a stand-in for command-specific success data. It is a JSON
// object and implements HumanRenderable, mirroring how real command data
// participates in the framework without the renderers knowing its type.
type sampleData struct {
	Value string `json:"value"`
}

func (d sampleData) RenderHuman(w io.Writer) error {
	_, err := fmt.Fprintln(w, d.Value)
	return err
}

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

func TestEnvelopeSchemaRejectsInvalidEnvelopes(t *testing.T) {
	cases := map[string]string{
		"ok true without data":     `{"ok":true,"command":"get","schemaVersion":1,"warnings":[]}`,
		"ok true with error":       `{"ok":true,"command":"get","schemaVersion":1,"data":{},"warnings":[],"error":{"code":"x","message":"y"}}`,
		"ok false without error":   `{"ok":false,"command":"get","schemaVersion":1,"warnings":[]}`,
		"ok false with data":       `{"ok":false,"command":"get","schemaVersion":1,"data":{},"warnings":[],"error":{"code":"x","message":"y"}}`,
		"missing warnings":         `{"ok":true,"command":"get","schemaVersion":1,"data":{}}`,
		"warnings not array":       `{"ok":true,"command":"get","schemaVersion":1,"data":{},"warnings":{}}`,
		"warnings null":            `{"ok":true,"command":"get","schemaVersion":1,"data":{},"warnings":null}`,
		"unknown command":          `{"ok":true,"command":"frobnicate","schemaVersion":1,"data":{},"warnings":[]}`,
		"wrong schema version":     `{"ok":true,"command":"get","schemaVersion":2,"data":{},"warnings":[]}`,
		"unknown top-level field":  `{"ok":true,"command":"get","schemaVersion":1,"data":{},"warnings":[],"extra":true}`,
		"warning missing message":  `{"ok":true,"command":"get","schemaVersion":1,"data":{},"warnings":[{"code":"x"}]}`,
		"warning unknown field":    `{"ok":true,"command":"get","schemaVersion":1,"data":{},"warnings":[{"code":"x","message":"y","z":1}]}`,
		"error empty code":         `{"ok":false,"command":"get","schemaVersion":1,"warnings":[],"error":{"code":"","message":"y"}}`,
		"error unknown field":      `{"ok":false,"command":"get","schemaVersion":1,"warnings":[],"error":{"code":"x","message":"y","z":1}}`,
		"error code uppercase":     `{"ok":false,"command":"get","schemaVersion":1,"warnings":[],"error":{"code":"SealConflict","message":"y"}}`,
		"error code underscore":    `{"ok":false,"command":"get","schemaVersion":1,"warnings":[],"error":{"code":"seal_conflict","message":"y"}}`,
		"error code trailing dash": `{"ok":false,"command":"get","schemaVersion":1,"warnings":[],"error":{"code":"seal-","message":"y"}}`,
		"warning code uppercase":   `{"ok":true,"command":"get","schemaVersion":1,"data":{},"warnings":[{"code":"StaleMetadata","message":"y"}]}`,
		"warning code underscore":  `{"ok":true,"command":"get","schemaVersion":1,"data":{},"warnings":[{"code":"stale_metadata","message":"y"}]}`,
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			requireViolatesEnvelopeSchema(t, env)
		})
	}
}

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
	// warnings is always an array, even when none were supplied.
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
	// Human success output must not be a JSON envelope.
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

func TestNormalizeWarningsNeverReturnsNil(t *testing.T) {
	if got := normalizeWarnings(nil); got == nil || len(got) != 0 {
		t.Fatalf("normalizeWarnings(nil) = %v, want empty non-nil slice", got)
	}
	// A non-nil slice must be returned unchanged rather than reallocated.
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

func TestHumanRendererSkipsNonRenderableData(t *testing.T) {
	// Data that does not implement HumanRenderable produces no stdout; warnings
	// still go to stderr.
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

// failWriter fails every write, so tests can exercise the renderers' and
// encoder's write-error return paths.
type failWriter struct{}

func (failWriter) Write(p []byte) (int, error) { return 0, fmt.Errorf("write failed") }

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

func TestEnvelopeOmitsDataWhenNilEvenThoughSchemaRejectsIt(t *testing.T) {
	// A success result with nil Data is a programming error: encodeEnvelope must
	// catch it via schema validation rather than emit a malformed envelope.
	_, err := encodeEnvelope(Envelope{
		OK:            true,
		Command:       commandGet,
		SchemaVersion: envelopeSchemaVersion,
		Warnings:      []Warning{},
	})
	if err == nil {
		t.Fatal("encodeEnvelope with nil data on success = nil error, want schema violation")
	}
}

func TestWriteEnvelopePropagatesEncodeError(t *testing.T) {
	// ok:true without data violates the schema, so encodeEnvelope fails and
	// writeEnvelope must propagate the error without writing partial output.
	var buf bytes.Buffer
	err := writeEnvelope(&buf, Envelope{
		OK:            true,
		Command:       commandGet,
		SchemaVersion: envelopeSchemaVersion,
		Warnings:      []Warning{},
	})
	if err == nil {
		t.Fatal("writeEnvelope with invalid envelope = nil, want error")
	}
	if buf.Len() != 0 {
		t.Fatalf("buf = %q, want empty on encode failure", buf.String())
	}
}

// TestSchemaCommandEnumMatchesAllCommands guards against the Go command enum and
// the schema's command enum drifting apart.
func TestSchemaCommandEnumMatchesAllCommands(t *testing.T) {
	var schemaDoc struct {
		Properties struct {
			Command struct {
				Enum []string `json:"enum"`
			} `json:"command"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(envelopeSchemaJSON, &schemaDoc); err != nil {
		t.Fatalf("parse envelope schema: %v", err)
	}

	want := make([]string, len(allCommands))
	for i, c := range allCommands {
		want[i] = string(c)
	}
	got := append([]string(nil), schemaDoc.Properties.Command.Enum...)

	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Fatalf("schema command enum %v does not match allCommands %v", got, want)
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
