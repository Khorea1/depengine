package app

import (
	"encoding/json"
	"io"
)

// machineOutputVersion is the envelope version for every `--format json`
// payload. It is independent of the file-format versions in
// docs/compatibility.md.
const machineOutputVersion = 1

// machineEnvelope is the single machine-output contract: a version, the
// command that produced it, and the command-specific payload under `data`.
// Keeping the payload nested means new fields never collide with envelope
// metadata and consumers can branch on `version` before parsing `data`.
type machineEnvelope struct {
	Version int    `json:"version"`
	Command string `json:"command"`
	Data    any    `json:"data"`
}

// writeMachineOutput renders one envelope as indented JSON.
func writeMachineOutput(w io.Writer, command string, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(machineEnvelope{Version: machineOutputVersion, Command: command, Data: data})
}

// normalizeOutputFormat folds the --json shorthand into --format and validates
// the value. Commands with extra formats (graph, sbom) validate their own.
func normalizeOutputFormat(format *string, jsonFlag *bool) error {
	if *jsonFlag {
		*format = "json"
	}
	switch *format {
	case "", "text":
		*format = "text"
		return nil
	case "json":
		return nil
	default:
		return &outputFormatError{value: *format}
	}
}

type outputFormatError struct{ value string }

func (e *outputFormatError) Error() string {
	return "unknown output format " + e.value + " (valid: text, json)"
}
