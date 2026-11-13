package sandbox

import (
	"fmt"
	"unicode"
)

// validateInputChars rejects caller-supplied fields containing characters that can break
// out of a scalar in the rendered blueprint YAML. The blueprint is rendered with
// text/template (no escaping), so a newline plus indentation in e.g. Image or
// metadata.runtimeClassName would inject sibling keys into the pod spec.
//
// Env var values are exempt: the blueprint quotes them with printf "%q", and multi-line
// values (certificates, keys) are legitimate. Env var names are still checked.
func (sb *Sandbox) validateInputChars() error {
	fields := map[string]string{
		"name":         sb.Name,
		"template":     sb.Template,
		"image":        sb.Image,
		"cmd":          sb.Cmd,
		"user":         sb.User,
		"app":          sb.App,
		"workdir":      sb.Workdir,
		"cpu":          sb.CPU,
		"memory":       sb.Memory,
		"cpu_limit":    sb.CPULimit,
		"memory_limit": sb.MemoryLimit,
	}
	for field, v := range fields {
		if err := checkSafeChars(field, v); err != nil {
			return err
		}
	}
	for i, a := range sb.Args {
		if err := checkSafeChars(fmt.Sprintf("args[%d]", i), a); err != nil {
			return err
		}
	}
	for k := range sb.EnvVars {
		if err := checkSafeChars("envVars key", k); err != nil {
			return err
		}
	}
	// Any metadata key may be referenced by an admin-edited blueprint, so check all of them.
	for k, v := range sb.Metadata {
		if err := checkSafeChars("metadata key", k); err != nil {
			return err
		}
		if err := checkSafeChars(fmt.Sprintf("metadata[%q]", k), v); err != nil {
			return err
		}
	}
	return nil
}

// checkSafeChars rejects control characters (incl. \n, \r, \t, NEL U+0085) and the
// Unicode line/paragraph separators U+2028/U+2029, which YAML parsers also treat as line breaks.
func checkSafeChars(field, v string) error {
	for _, r := range v {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("invalid %s: control or line-break characters are not allowed", field)
		}
	}
	return nil
}
