package main

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/output"
)

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

func requireViolatesEnvelopeSchema(t *testing.T, jsonOutput string) {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(jsonOutput))
	if err != nil {
		return
	}
	if err := output.Schema.Validate(inst); err == nil {
		t.Fatalf("json output unexpectedly conforms to envelope schema:\n%s", jsonOutput)
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

func TestEnvelopeOmitsDataWhenNilEvenThoughSchemaRejectsIt(t *testing.T) {
	_, err := output.EncodeEnvelope(output.Envelope{
		OK:            true,
		Command:       output.CommandGet,
		SchemaVersion: output.SchemaVersion,
		Warnings:      []output.Warning{},
	})
	if err == nil {
		t.Fatal("EncodeEnvelope with nil data on success = nil error, want schema violation")
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
