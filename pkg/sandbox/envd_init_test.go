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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPostEnvdInitSendsTokenAndEnvVars(t *testing.T) {
	var got envdInitRequest
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/init" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotHeader = r.Header.Get("X-Access-Token")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	dest, _ := url.Parse(srv.URL)
	payload, _ := json.Marshal(envdInitRequest{EnvVars: map[string]string{"K": "V"}, AccessToken: "tok"})

	if err := postEnvdInit(dest, "tok", payload); err != nil {
		t.Fatalf("postEnvdInit: %v", err)
	}
	if gotHeader != "tok" || got.AccessToken != "tok" || got.EnvVars["K"] != "V" {
		t.Fatalf("unexpected init request: header=%q body=%+v", gotHeader, got)
	}
}

func TestPostEnvdInitStatusMapping(t *testing.T) {
	cases := []struct {
		status       int
		wantErr      bool
		wantConflict bool
	}{
		{http.StatusNoContent, false, false},
		{http.StatusConflict, true, true},
		{http.StatusUnauthorized, true, true},
		{http.StatusBadGateway, true, false},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
		}))
		dest, _ := url.Parse(srv.URL)
		err := postEnvdInit(dest, "tok", []byte("{}"))
		srv.Close()

		if (err != nil) != c.wantErr {
			t.Errorf("status %d: err=%v, wantErr=%v", c.status, err, c.wantErr)
		}
		if errors.Is(err, errEnvdTokenConflict) != c.wantConflict {
			t.Errorf("status %d: conflict=%v, want %v", c.status, errors.Is(err, errEnvdTokenConflict), c.wantConflict)
		}
	}
}
