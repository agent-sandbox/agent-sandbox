/*
 * Copyright 2025 The https://github.com/agent-sandbox/agent-sandbox Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package e2b

import (
	"fmt"
	"strings"
	"testing"

	"github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
)

func TestRedactNewSandbox(t *testing.T) {
	n := &api.NewSandbox{TemplateID: "base", EnvVars: map[string]string{"OPENAI_API_KEY": "sk-secret"}}

	got := fmt.Sprintf("%+v", redactNewSandbox(n))

	if strings.Contains(got, "sk-secret") {
		t.Errorf("logged request contains an env var value: %s", got)
	}
	if !strings.Contains(got, "OPENAI_API_KEY") || !strings.Contains(got, "TemplateID:base") {
		t.Errorf("logged request = %s, want env var names and other fields kept", got)
	}
	if n.EnvVars["OPENAI_API_KEY"] != "sk-secret" {
		t.Errorf("original request modified: %v", n.EnvVars)
	}
	if redactNewSandbox(nil) != nil {
		t.Errorf("redactNewSandbox(nil) should be nil")
	}
}
