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

package sandbox

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestSandboxStringRedactsSecrets(t *testing.T) {
	sb := GetDefaultSandbox()
	sb.Name = "sbx-test"
	sb.User = "user-0123456789abcdef"
	sb.EnvVars = map[string]string{"OPENAI_API_KEY": "sk-secret"}
	sb.Metadata["snapshot"] = "c2stc2VjcmV0"
	// a real ReplicaSet carries the raw sandbox in sandbox-data and the env in its containers
	sb.ReplicaSet.Name = "sbx-test-rs"
	sb.ReplicaSet.Annotations[AnnotationSandboxData] = `{"user":"user-0123456789abcdef","envVars":{"OPENAI_API_KEY":"sk-secret"}}`
	sb.ReplicaSet.Spec.Template.Spec.Containers = []corev1.Container{{
		Name: "sandbox",
		Env:  []corev1.EnvVar{{Name: "OPENAI_API_KEY", Value: "sk-secret"}},
	}}

	got := fmt.Sprintf("Create sandbox opts %v", sb)

	if strings.Contains(got, sb.User) {
		t.Errorf("sandbox output contains the full user key: %s", got)
	}
	if strings.Contains(got, "sk-secret") {
		t.Errorf("sandbox output contains an env var value: %s", got)
	}
	if strings.Contains(got, "c2stc2VjcmV0") {
		t.Errorf("sandbox output contains the process snapshot: %s", got)
	}
	for _, want := range []string{"sbx-test", "OPENAI_API_KEY", MaskUserKey(sb.User)} {
		if !strings.Contains(got, want) {
			t.Errorf("sandbox output missing %q: %s", want, got)
		}
	}
	if sb.User != "user-0123456789abcdef" || sb.EnvVars["OPENAI_API_KEY"] != "sk-secret" ||
		sb.Metadata["snapshot"] != "c2stc2VjcmV0" || sb.ReplicaSet == nil {
		t.Errorf("String modified the sandbox: %+v", *sb)
	}
}

func TestSandboxToStringRedactsSecrets(t *testing.T) {
	sb := GetDefaultSandbox()
	sb.User = "user-0123456789abcdef"
	sb.EnvVars = map[string]string{"OPENAI_API_KEY": "sk-secret"}

	got := sb.ToString()

	if strings.Contains(got, sb.User) || strings.Contains(got, "sk-secret") {
		t.Errorf("ToString contains a secret: %s", got)
	}
	if !strings.Contains(got, "OPENAI_API_KEY") {
		t.Errorf("ToString = %s, want env var names kept", got)
	}
}
