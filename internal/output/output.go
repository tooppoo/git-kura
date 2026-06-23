// Package output implements the structured output framework for git kura commands.
//
// Command executions do not write display strings to stdout/stderr directly.
// They build a Result (on success) or return a CommandError (on failure). A
// Renderer turns that intermediate representation into output: the human
// renderer writes existing-style text, the JSON renderer writes a
// schema-valid common Envelope, and the TOON renderer writes the same
// validated Envelope in Token-Oriented Object Notation for AI-friendly output.
//
// Renderers never depend on command-specific types: they treat a Result's Data
// and a CommandError's Details as opaque values, and delegate human formatting
// of Data to the HumanRenderable interface that command-specific data
// implements.
//
// This package must not depend on Cobra command objects, Cobra command trees,
// or os.Stdout / os.Stderr. Command adapters pass explicit io.Writer values
// when calling renderers, and pass an explicit Command identifier (an
// output-contract identifier, not a value derived from the CLI structure) when
// building Result or CommandError values.
//
// Command identifier constants are transitional residents of this package: in
// the long term they may move to internal/cli or to individual command
// adapters once internal/cli, internal/seal, and internal/tools are separated
// and internal/output knowing every command becomes an undesirable dependency.
package output

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	toon "github.com/toon-format/toon-go"
)

// SchemaVersion is the schema version stamped into every structured output
// envelope. It versions the common envelope shape only; command-specific data
// and error.details schemas version independently.
const SchemaVersion = 1

//go:embed schema/envelope.schema.json
var SchemaJSON []byte

// Schema is the compiled envelope JSON Schema used to validate every envelope
// before it is emitted. A validation failure is an internal error: the command
// produced output that violates the envelope contract.
var Schema = mustCompileEnvelopeSchema()

func mustCompileEnvelopeSchema() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(SchemaJSON))
	if err != nil {
		panic(fmt.Sprintf("parse envelope schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("envelope.schema.json", doc); err != nil {
		panic(fmt.Sprintf("add envelope schema resource: %v", err))
	}
	sch, err := c.Compile("envelope.schema.json")
	if err != nil {
		panic(fmt.Sprintf("compile envelope schema: %v", err))
	}
	return sch
}

// Command is the canonical identifier for a command execution in structured
// output. It is dot-separated so subcommands stay grouped under their parent
// command (for example "seal.claim"). The schema's command enum must list
// exactly the values in AllCommands.
type Command string

const (
	CommandGet         Command = "get"
	CommandOpen        Command = "open"
	CommandClose       Command = "close"
	CommandLs          Command = "ls"
	CommandSealClaim   Command = "seal.claim"
	CommandSealUnclaim Command = "seal.unclaim"
	CommandSealTest    Command = "seal.test"
	CommandSealLs      Command = "seal.ls"
	CommandSealDoctor  Command = "seal.doctor"
)

// AllCommands is the canonical set of command identifiers. It is the source of
// truth that the envelope schema's command enum mirrors; a test cross-checks
// the two so they cannot drift.
var AllCommands = []Command{
	CommandGet,
	CommandOpen,
	CommandClose,
	CommandLs,
	CommandSealClaim,
	CommandSealUnclaim,
	CommandSealTest,
	CommandSealLs,
	CommandSealDoctor,
}

// Warning is a non-fatal diagnostic attached to a command execution. Warnings
// are reported in both successful and failed envelopes. Details carries
// optional command-specific structure and is treated as opaque by renderers.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// envelopeError is the machine-readable error of a failed command. Code uses
// the same hyphen-case reason tokens as the stderr error output and the
// exit-code names. Details carries optional command-specific structure and is
// treated as opaque by renderers.
type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// Envelope is the common JSON output wrapper. Exactly one of Data / Error is
// present, selected by OK; Warnings is always an array.
type Envelope struct {
	OK            bool           `json:"ok"`
	Command       Command        `json:"command"`
	SchemaVersion int            `json:"schemaVersion"`
	Data          any            `json:"data,omitempty"`
	Warnings      []Warning      `json:"warnings"`
	Error         *envelopeError `json:"error,omitempty"`
}

// Result is the structured outcome of a successful command execution. Commands
// build a Result instead of writing display strings directly; renderers turn it
// into human or JSON output. Data is the command-specific success payload and
// becomes the envelope's data; it must be a JSON object. A failed command
// returns a CommandError instead of a Result.
type Result struct {
	Command  Command
	Data     any
	Warnings []Warning
}

// CommandError is the intermediate representation of a failed command
// execution. It carries everything both renderers need: the human stderr
// Message, the hyphen-case Code, the process ExitCode, and optional
// machine-readable Details.
//
// In non-JSON mode the human renderer writes Message to stderr, preserving the
// existing stderr error output; in JSON mode the JSON renderer converts it
// into an error envelope on stdout. Either way main exits with ExitCode.
type CommandError struct {
	Command  Command
	Code     string
	Message  string
	ExitCode int
	Details  any
	Warnings []Warning
}

func (e *CommandError) Error() string { return e.Message }

// HumanRenderable is implemented by command-specific data that knows how to
// render its own human-readable representation. The human renderer delegates
// to it so the renderer itself stays free of command-specific types.
type HumanRenderable interface {
	RenderHuman(w io.Writer) error
}

// Renderer turns a structured Result or CommandError into output on the given
// writers. Implementations must not depend on command-specific data types:
// they treat Data and Details as opaque values.
type Renderer interface {
	RenderResult(stdout, stderr io.Writer, r Result) error
	RenderError(stdout, stderr io.Writer, e *CommandError) error
}

// RenderMode selects between human-readable, JSON, and TOON structured output.
// It is the renderer-relevant projection of the CLI output mode; scalar modes
// never reach a renderer.
type RenderMode int

const (
	RenderHuman RenderMode = iota
	RenderJSON
	RenderTOON
)

// SelectRenderer returns the Renderer for the given mode.
func SelectRenderer(mode RenderMode) Renderer {
	switch mode {
	case RenderJSON:
		return jsonRenderer{}
	case RenderTOON:
		return toonRenderer{}
	default:
		return humanRenderer{}
	}
}

// humanRenderer writes existing-style human output: command-specific success
// text on stdout (delegated to HumanRenderable), warnings and errors on stderr.
type humanRenderer struct{}

func (humanRenderer) RenderResult(stdout, stderr io.Writer, r Result) error {
	if hr, ok := r.Data.(HumanRenderable); ok {
		if err := hr.RenderHuman(stdout); err != nil {
			return err
		}
	}
	for _, w := range r.Warnings {
		if _, err := fmt.Fprintf(stderr, "warning: %s\n", w.Message); err != nil {
			return err
		}
	}
	return nil
}

func (humanRenderer) RenderError(stdout, stderr io.Writer, e *CommandError) error {
	for _, w := range e.Warnings {
		if _, err := fmt.Fprintf(stderr, "warning: %s\n", w.Message); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stderr, e.Message)
	return err
}

// jsonRenderer writes a schema-valid common Envelope to stdout for both
// success and failure.
type jsonRenderer struct{}

func (jsonRenderer) RenderResult(stdout, stderr io.Writer, r Result) error {
	return WriteEnvelope(stdout, Envelope{
		OK:            true,
		Command:       r.Command,
		SchemaVersion: SchemaVersion,
		Data:          r.Data,
		Warnings:      NormalizeWarnings(r.Warnings),
	})
}

func (jsonRenderer) RenderError(stdout, stderr io.Writer, e *CommandError) error {
	return WriteEnvelope(stdout, Envelope{
		OK:            false,
		Command:       e.Command,
		SchemaVersion: SchemaVersion,
		Warnings:      NormalizeWarnings(e.Warnings),
		Error: &envelopeError{
			Code:    e.Code,
			Message: e.Message,
			Details: e.Details,
		},
	})
}

// toonRenderer writes a schema-valid common Envelope in TOON format to stdout
// for both success and failure. It reuses the same EncodeEnvelope validation
// helper as jsonRenderer; the validated JSON bytes are then decoded to a
// generic map and re-encoded as TOON so that field names follow the JSON tag
// conventions (camelCase) rather than Go field names.
//
// TOON output is experimental: its whitespace, line breaks, and field order
// are not part of the stable contract. The stable contract is the JSON
// envelope and the command-specific JSON Schemas.
type toonRenderer struct{}

func (toonRenderer) RenderResult(stdout, stderr io.Writer, r Result) error {
	return writeTOONEnvelope(stdout, Envelope{
		OK:            true,
		Command:       r.Command,
		SchemaVersion: SchemaVersion,
		Data:          r.Data,
		Warnings:      NormalizeWarnings(r.Warnings),
	})
}

func (toonRenderer) RenderError(stdout, stderr io.Writer, e *CommandError) error {
	return writeTOONEnvelope(stdout, Envelope{
		OK:            false,
		Command:       e.Command,
		SchemaVersion: SchemaVersion,
		Warnings:      NormalizeWarnings(e.Warnings),
		Error: &envelopeError{
			Code:    e.Code,
			Message: e.Message,
			Details: e.Details,
		},
	})
}

// writeTOONEnvelope encodes and validates e via the shared EncodeEnvelope
// helper, then converts the validated JSON representation to TOON. Converting
// through the JSON bytes (rather than marshalling the Envelope struct directly)
// preserves camelCase field names from JSON struct tags and ensures all values
// are basic Go types that the TOON encoder handles without special cases.
func writeTOONEnvelope(w io.Writer, e Envelope) error {
	jsonBytes, err := EncodeEnvelope(e)
	if err != nil {
		return err
	}
	var decoded any
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		return fmt.Errorf("internal: decode envelope for toon: %w", err)
	}
	out, err := toon.MarshalString(decoded)
	if err != nil {
		return fmt.Errorf("internal: toon encoding failed: %w", err)
	}
	_, err = fmt.Fprintln(w, out)
	return err
}

// NormalizeWarnings guarantees a non-nil slice so warnings marshals to a JSON
// array ([]) rather than null, on both success and failure.
func NormalizeWarnings(warnings []Warning) []Warning {
	if warnings == nil {
		return []Warning{}
	}
	return warnings
}

// EncodeEnvelope marshals an Envelope to JSON and validates it against
// schema/envelope.schema.json before it is emitted. A validation failure is
// an internal error: it means a command produced output that violates the
// envelope contract.
func EncodeEnvelope(e Envelope) ([]byte, error) {
	out, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("internal: marshal envelope: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("internal: parse envelope: %w", err)
	}
	if err := Schema.Validate(inst); err != nil {
		return nil, fmt.Errorf("internal: json output does not conform to envelope schema: %w", err)
	}
	return out, nil
}

// WriteEnvelope encodes, validates, and writes an Envelope to w.
func WriteEnvelope(w io.Writer, e Envelope) error {
	out, err := EncodeEnvelope(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}
