package sandbox

import (
	"strings"
	"testing"
)

// validateInputChars guards blueprint YAML rendered with text/template (no escaping).
// A value is legal as long as it stays on one line; any control or line-break
// character is rejected because it could inject sibling keys into the pod spec.
//
// Legal examples:
//
//	image:   "python:3.12-slim"
//	cmd:     `echo "hi" && ls -la /tmp # ok`   // quotes, shell and YAML symbols on one line
//	name:    "沙箱-éß-📦"                        // non-ASCII letters and emoji
//	envVars: {"CERT": "-----BEGIN...\n...END-----\n"} // env var VALUES are exempt (quoted with %q)
//
// Illegal examples:
//
//	image:    "python:3.12\n  privileged: true" // \n would inject "privileged: true" into the pod spec
//	cmd:      "sh\r", "a\tb", "a\x00b"           // \r, \t, NUL and other C0 controls
//	workdir:  "/home\u0085x"                     // NEL (C1 control), a YAML line break
//	app:      "demo x", "demo x"       // Unicode line/paragraph separators
//	envVars:  {"BAD\nKEY": "v"}                  // env var KEYS are still checked
//	metadata: {"runtimeClassName": "x\n  y: z"}  // metadata keys and values are both checked

func TestCheckSafeChars(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty", "", false},
		{"plain ascii", "python:3.12-slim", false},
		{"spaces and symbols", `echo "hi" && ls -la /tmp # ok`, false},
		{"yaml-ish chars on one line", "a: b, {c: d}, [e]", false},
		{"non-ascii letters", "沙箱-éß-日本", false},
		{"emoji", "box-📦", false},
		{"newline", "img\n  privileged: true", true},
		{"carriage return", "img\r", true},
		{"tab", "a\tb", true},
		{"nul", "a\x00b", true},
		{"escape", "a\x1bb", true},
		{"del", "a\x7fb", true},
		{"NEL U+0085", "a\u0085b", true},
		{"C1 control U+009F", "a\u009fb", true},
		{"line separator U+2028", "a b", true},
		{"paragraph separator U+2029", "a b", true},
		// Zero-width / format chars are not line breaks, so they are allowed.
		{"zero-width space U+200B", "a​b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkSafeChars("field", tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkSafeChars(%q) err = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "invalid field") {
				t.Errorf("error %q should name the field", err)
			}
		})
	}
}

// validSandbox returns a sandbox with every validated field populated with safe values.
func validSandbox() *Sandbox {
	return &Sandbox{
		SandboxBase: SandboxBase{Name: "sb-1", Template: "python"},
		Image:       "python:3.12",
		Cmd:         "/bin/sh",
		Args:        []string{"-c", "echo hello"},
		User:        "alice",
		App:         "demo",
		Workdir:     "/home/user",
		CPU:         "200m",
		Memory:      "500Mi",
		CPULimit:    "1000m",
		MemoryLimit: "2000Mi",
		EnvVars:     map[string]string{"FOO": "bar"},
		Metadata:    map[string]string{"team": "infra"},
	}
}

func TestValidateInputChars_Valid(t *testing.T) {
	if err := validSandbox().validateInputChars(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := (&Sandbox{}).validateInputChars(); err != nil {
		t.Fatalf("empty sandbox: unexpected error: %v", err)
	}
}

func TestValidateInputChars_EnvVarValueExempt(t *testing.T) {
	sb := validSandbox()
	// Multi-line env values (certs, keys) are legitimate; the blueprint quotes them.
	sb.EnvVars["CERT"] = "-----BEGIN CERTIFICATE-----\nMIIB\r\n\t-----END CERTIFICATE-----\n"
	if err := sb.validateInputChars(); err != nil {
		t.Fatalf("env var values must be exempt, got: %v", err)
	}
}

// Mirrors the shape of a real business submission (secrets replaced with placeholders):
// multi-line Markdown with emoji/CJK, JSON with escaped quotes, base64 and %-encoded URLs
// in env var values, plus a login-shell cmd/args. None of it may be rejected.
func TestValidateInputChars_RealisticBusinessPayload(t *testing.T) {
	sb := validSandbox()
	sb.Cmd = "/bin/bash"
	sb.Args = []string{"-l", "-c", "aiready finish"}
	sb.Workdir = "/mnt/workspace"
	sb.EnvVars = map[string]string{
		"AIREADY_TASK_SUMMARY":          "### 📋 核心主题\n本文围绕胃癌围术期治疗进展。\n\n### 🏷️ 关键词\n免疫治疗 | 精准分层\n\n**第一部分**\n- 关键信息：pCR常低于10%；HR约0.64–0.67。",
		"AIREADY_STEP_AUTH":             `{"extract": {"appkey": "app", "secret": "<redacted>"}}`,
		"AIREADY_TASK_STEP_SUBMIT_URLS": `{"extract": "https://example.com/extract/submit?lane=blue"}`,
		"AIREADY_TASK_METADATA":         "H4sIAFHN+moC/zWPvU7DQBCEXyW6mkW+857to4uoaACJdIjifvaiE4lt2QkSINqIggIaREETiZaIko6XwSSPgZ043e7OzOqbe1YX88rS6LYkdjSIDgbsOh+fuHZm0vgIbRyBQSEBUyNAxUhgdcZbScQZd6wNhNxO5o6Gs1lVt7lLVhZ1mIUirzu1tpTrKhTbZW6nIz3ejibcnfc+dtVJNVWnetpRsPVquX5eNC9vbMfT04netsPD1GdKJSkongpAr3yLRwqM1ElEHLWyusv7MNnXYzfBUdEdi6ovyVPLY0wdEHnXluQxZEmiwUnyhlB7I5L9lz5BSkmtBUQKFaAmAZpLAdI7EWUkEp5hz73vM7w4Pvv9/mw+Xpv3xd/yabP62Xw9Hk5LZA//mMAE74IBAAA=",
		"AIREADY_TASK_FILE_URL":         "https://cdn.example.com/v.mp4?responseContentDisposition=attachment%3B%20filename%3DASCO%E4%B8%AD.mp4&authorization=bce-auth-v1%2Fabc%2F2026-11-16T09%3A30%3A57Z",
		"AIREADY_TASK_OSS_PREFIX":       "/knowmine/17c1347d-eefd-4713-866a-d5efbe4afb26/x_aia",
		"DEBUG":                         "True",
		"context_id":                    "a6e7bf10-35d7-4d72-b5db-2702f56513fd",
	}
	if err := sb.validateInputChars(); err != nil {
		t.Fatalf("realistic business payload must pass, got: %v", err)
	}
}

func TestValidateInputChars_Rejects(t *testing.T) {
	const bad = "x\n  privileged: true"
	tests := []struct {
		name      string
		mutate    func(sb *Sandbox)
		wantField string
	}{
		{"name", func(sb *Sandbox) { sb.Name = bad }, "invalid name"},
		{"template", func(sb *Sandbox) { sb.Template = bad }, "invalid template"},
		{"image", func(sb *Sandbox) { sb.Image = bad }, "invalid image"},
		{"cmd", func(sb *Sandbox) { sb.Cmd = bad }, "invalid cmd"},
		{"user", func(sb *Sandbox) { sb.User = bad }, "invalid user"},
		{"app", func(sb *Sandbox) { sb.App = bad }, "invalid app"},
		{"workdir", func(sb *Sandbox) { sb.Workdir = bad }, "invalid workdir"},
		{"cpu", func(sb *Sandbox) { sb.CPU = bad }, "invalid cpu"},
		{"memory", func(sb *Sandbox) { sb.Memory = bad }, "invalid memory"},
		{"cpu_limit", func(sb *Sandbox) { sb.CPULimit = bad }, "invalid cpu_limit"},
		{"memory_limit", func(sb *Sandbox) { sb.MemoryLimit = bad }, "invalid memory_limit"},
		{"args index", func(sb *Sandbox) { sb.Args = append(sb.Args, bad) }, "invalid args[2]"},
		{"env var key", func(sb *Sandbox) { sb.EnvVars["BAD\nKEY"] = "v" }, "invalid envVars key"},
		{"metadata key", func(sb *Sandbox) { sb.Metadata["bad key"] = "v" }, "invalid metadata key"},
		{"metadata value", func(sb *Sandbox) { sb.Metadata["runtimeClassName"] = bad }, `invalid metadata["runtimeClassName"]`},
		{"unicode line separator in image", func(sb *Sandbox) { sb.Image = "img x" }, "invalid image"},
		{"NEL in cmd", func(sb *Sandbox) { sb.Cmd = "sh\u0085x" }, "invalid cmd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := validSandbox()
			tt.mutate(sb)
			err := sb.validateInputChars()
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantField) {
				t.Errorf("error %q should contain %q", err, tt.wantField)
			}
		})
	}
}
