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

package commandaudit

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildProcessStartRecordExtractsShellScript(t *testing.T) {
	body := []byte(`{"process":{"cmd":"/bin/bash","args":["-lc","echo ok"],"cwd":"/workspace"},"tag":"cmd"}`)

	record, ok := buildProcessStartRecord(SandboxInfo{TenantID: "user-a", SandboxID: "sbx1", SandboxName: "sandbox-a"}, "/process.Process/Start", "49999", body)
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.CommandText != "echo ok" {
		t.Fatalf("command text = %q, want %q", record.CommandText, "echo ok")
	}
	if record.Cwd != "/workspace" {
		t.Fatalf("cwd = %q, want /workspace", record.Cwd)
	}
	if record.Source != "commands.run" {
		t.Fatalf("source = %q, want commands.run", record.Source)
	}
}

func TestBuildProcessStartRecordSupportsConnectJSON(t *testing.T) {
	payload := []byte(`{"process":{"cmd":"python","args":["-m","http.server","8000"]}}`)
	var body bytes.Buffer
	body.WriteByte(0)
	_ = binary.Write(&body, binary.BigEndian, uint32(len(payload)))
	body.Write(payload)

	record, ok := buildProcessStartRecord(SandboxInfo{SandboxName: "sandbox-a"}, "/process.Process/Start", "49999", body.Bytes())
	if !ok {
		t.Fatal("expected audit record")
	}
	if record.CommandText != "python -m http.server 8000" {
		t.Fatalf("command text = %q", record.CommandText)
	}
}

func TestRecordProxyRequestRestoresRequestBody(t *testing.T) {
	previous := DefaultStore
	DefaultStore = NewStore(10)
	defer func() { DefaultStore = previous }()

	body := []byte(`{"process":{"cmd":"sh","args":["-c","whoami"]}}`)
	req := httptest.NewRequest(http.MethodPost, "/sandbox/sbx/process.Process/Start", bytes.NewReader(body))

	if err := RecordProxyRequest(req, SandboxInfo{SandboxName: "sbx"}, "/process.Process/Start", "49999"); err != nil {
		t.Fatal(err)
	}

	restored, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(body) {
		t.Fatalf("body was not restored: %q", restored)
	}

	items := DefaultStore.List(ListOptions{Limit: 10})
	if len(items) != 1 {
		t.Fatalf("record count = %d, want 1", len(items))
	}
	if items[0].CommandText != "whoami" {
		t.Fatalf("command text = %q, want whoami", items[0].CommandText)
	}
}
