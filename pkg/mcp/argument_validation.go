package mcp

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// argumentValidator checks tool-call arguments against the tool's input schema before
// the handler runs. The go-sdk's raw AddTool path does not, so a wrongly typed or
// unknown argument used to reach the handler, which could drop it silently (a dryRun
// argument that no tool has was ignored, and the write happened).
//
// Required arguments are left to the handlers, which already report them with their
// own messages; everything else in the schema (types, enums, patterns, ranges, nested
// objects) is enforced, and an argument the schema does not declare is refused.
type argumentValidator struct {
	resolved *jsonschema.Resolved
	known    []string
}

func newArgumentValidator(schema *jsonschema.Schema) (*argumentValidator, error) {
	clone := schema.CloneSchemas()
	clone.Required = nil
	resolved, err := clone.Resolve(nil)
	if err != nil {
		return nil, err
	}
	known := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		known = append(known, name)
	}
	slices.Sort(known)
	return &argumentValidator{resolved: resolved, known: known}, nil
}

func (v *argumentValidator) validate(arguments map[string]any) error {
	var unknown []string
	for name := range arguments {
		if !slices.Contains(v.known, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("unknown argument(s) %s; this tool accepts: %s", strings.Join(unknown, ", "), strings.Join(v.known, ", "))
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	if err := v.resolved.Validate(arguments); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}
