package main

import (
	"bytes"
	"strings"
	"testing"

	toon "github.com/toon-format/toon-go"
	"github.com/tooppoo/git-kura/internal/output"
)

// TestTOONRendererRendersSuccessEnvelope verifies that toonRenderer produces
// TOON output that contains all top-level envelope fields for a success result.
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

// TestTOONRendererRendersErrorEnvelope verifies that toonRenderer produces
// TOON output containing all top-level fields for an error result.
func TestTOONRendererRendersErrorEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := output.SelectRenderer(output.RenderTOON)
	if err := r.RenderError(&stdout, &stderr, &output.CommandError{
		Command:  output.CommandSealClaim,
		Code:     "seal-conflict",
		Message:  "path is already claimed",
		ExitCode: exitSealConflict,
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

// TestTOONRendererCarriesWarnings verifies that warnings appear in TOON output.
func TestTOONRendererCarriesWarnings(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := output.SelectRenderer(output.RenderTOON)
	if err := r.RenderResult(&stdout, &stderr, output.Result{
		Command:  output.CommandOpen,
		Data:     sampleData{Value: "x"},
		Warnings: []output.Warning{{Code: "stale-metadata", Message: "metadata is stale"}},
	}); err != nil {
		t.Fatalf("RenderResult: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "warnings") {
		t.Fatalf("TOON output = %q, want it to contain 'warnings'", out)
	}
	if !strings.Contains(out, "stale-metadata") {
		t.Fatalf("TOON output = %q, want it to contain warning code 'stale-metadata'", out)
	}
}

// TestTOONRendererDataFieldPresent verifies that command-specific data fields
// appear in TOON output.
func TestTOONRendererDataFieldPresent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := output.SelectRenderer(output.RenderTOON)
	data := sampleData{Value: "the-worktree-path"}
	if err := r.RenderResult(&stdout, &stderr, output.Result{
		Command: output.CommandGet,
		Data:    data,
	}); err != nil {
		t.Fatalf("RenderResult: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "data") {
		t.Fatalf("TOON output = %q, want it to contain 'data'", out)
	}
	if !strings.Contains(out, "the-worktree-path") {
		t.Fatalf("TOON output = %q, want it to contain the data value", out)
	}
}

// TestTOONRendererIsDecodable verifies that TOON output can be decoded back to
// a generic map and that all top-level envelope keys are present.
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
			t.Fatalf("decoded TOON missing key %q; keys present: %v", key, mapKeys(obj))
		}
	}

	if obj["ok"] != true {
		t.Fatalf("ok = %v, want true", obj["ok"])
	}
	if obj["command"] != "ls" {
		t.Fatalf("command = %v, want ls", obj["command"])
	}
}

// TestTOONRendererErrorIsDecodable verifies that a TOON error envelope can be
// decoded and contains an "error" key with "code" and "message".
func TestTOONRendererErrorIsDecodable(t *testing.T) {
	var stdout bytes.Buffer
	r := output.SelectRenderer(output.RenderTOON)
	if err := r.RenderError(&stdout, &bytes.Buffer{}, &output.CommandError{
		Command:  output.CommandClose,
		Code:     "unsafe-refused",
		Message:  "dirty worktree",
		ExitCode: exitUnsafeRefused,
	}); err != nil {
		t.Fatalf("RenderError: %v", err)
	}

	decoded, err := toon.DecodeString(strings.TrimSpace(stdout.String()))
	if err != nil {
		t.Fatalf("toon.DecodeString: %v", err)
	}

	obj, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("decoded = %T, want map[string]any", decoded)
	}

	if obj["ok"] != false {
		t.Fatalf("ok = %v, want false", obj["ok"])
	}

	errObj, ok := obj["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %T, want map[string]any", obj["error"])
	}
	if errObj["code"] != "unsafe-refused" {
		t.Fatalf("error.code = %v, want unsafe-refused", errObj["code"])
	}
}

// TestTOONAndJSONRenderersProduceSameTopLevelFields verifies that JSON and TOON
// renderers expose the same top-level envelope keys (ok, command, schemaVersion,
// data, warnings), ensuring no information gap between the two formats.
func TestTOONAndJSONRenderersProduceSameTopLevelFields(t *testing.T) {
	result := output.Result{
		Command:  output.CommandGet,
		Data:     sampleData{Value: "path"},
		Warnings: []output.Warning{{Code: "w", Message: "note"}},
	}

	var jsonStdout, toonStdout bytes.Buffer
	if err := output.SelectRenderer(output.RenderJSON).RenderResult(&jsonStdout, &bytes.Buffer{}, result); err != nil {
		t.Fatalf("JSON RenderResult: %v", err)
	}
	if err := output.SelectRenderer(output.RenderTOON).RenderResult(&toonStdout, &bytes.Buffer{}, result); err != nil {
		t.Fatalf("TOON RenderResult: %v", err)
	}

	jsonEnv := decodeEnvelope(t, jsonStdout.String())

	toonDecoded, err := toon.DecodeString(strings.TrimSpace(toonStdout.String()))
	if err != nil {
		t.Fatalf("toon.DecodeString: %v", err)
	}
	toonEnv, ok := toonDecoded.(map[string]any)
	if !ok {
		t.Fatalf("toon decoded = %T, want map", toonDecoded)
	}

	for key := range jsonEnv {
		if _, exists := toonEnv[key]; !exists {
			t.Fatalf("TOON envelope missing key %q present in JSON envelope", key)
		}
	}
}

// TestTOONRendererPropagatesWriteError verifies that write errors are surfaced.
func TestTOONRendererPropagatesWriteError(t *testing.T) {
	r := output.SelectRenderer(output.RenderTOON)
	err := r.RenderResult(failWriter{}, &bytes.Buffer{}, output.Result{Command: output.CommandGet, Data: sampleData{Value: "x"}})
	if err == nil {
		t.Fatal("RenderResult on failing stdout = nil, want error")
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// requireTOONErrorMessageContains decodes a TOON error envelope and asserts that
// error.message contains every given substring.
func requireTOONErrorMessageContains(t *testing.T, toonOutput, command string, substrings ...string) {
	t.Helper()

	decoded, err := toon.DecodeString(strings.TrimSpace(toonOutput))
	if err != nil {
		t.Fatalf("requireTOONErrorMessageContains: DecodeString: %v\noutput: %s", err, toonOutput)
	}
	obj, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("requireTOONErrorMessageContains: decoded type %T, want map\noutput: %s", decoded, toonOutput)
	}
	if obj["ok"] != false {
		t.Fatalf("requireTOONErrorMessageContains: ok = %v, want false\noutput: %s", obj["ok"], toonOutput)
	}
	if obj["command"] != command {
		t.Fatalf("requireTOONErrorMessageContains: command = %v, want %q\noutput: %s", obj["command"], command, toonOutput)
	}
	errObj, ok := obj["error"].(map[string]any)
	if !ok {
		t.Fatalf("requireTOONErrorMessageContains: error field type %T, want map\noutput: %s", obj["error"], toonOutput)
	}
	message, ok := errObj["message"].(string)
	if !ok {
		t.Fatalf("requireTOONErrorMessageContains: error.message type %T, want string\noutput: %s", errObj["message"], toonOutput)
	}
	for _, want := range substrings {
		if !strings.Contains(message, want) {
			t.Fatalf("requireTOONErrorMessageContains: message = %q, want it to contain %q\noutput: %s", message, want, toonOutput)
		}
	}
}
