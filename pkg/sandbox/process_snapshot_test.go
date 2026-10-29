package sandbox

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	e2bapi "github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
)

func proc(cmd string, args []string, tag string) e2bapi.ProcessInfo {
	return e2bapi.ProcessInfo{Config: e2bapi.ProcessConfig{Cmd: cmd, Args: args, Cwd: "/home/user"}, Tag: tag}
}

func TestIsProcessRunning(t *testing.T) {
	running := map[string]bool{}
	for _, p := range []e2bapi.ProcessInfo{
		proc("/bin/bash", []string{"-l", "-c", "python -m http.server 8001"}, ""),
		proc("/bin/bash", []string{"-l", "-c", "node app.js"}, "web"),
	} {
		for _, k := range processKeys(p) {
			running[k] = true
		}
	}

	cases := []struct {
		name string
		p    e2bapi.ProcessInfo
		want bool
	}{
		{"same command line", proc("/bin/bash", []string{"-l", "-c", "python -m http.server 8001"}, ""), true},
		{"same tag, different command", proc("/bin/bash", []string{"-l", "-c", "node app2.js"}, "web"), true},
		{"different args", proc("/bin/bash", []string{"-l", "-c", "python -m http.server 8002"}, ""), false},
		{"args are not joined ambiguously", proc("/bin/bash", []string{"-l -c", "python -m http.server 8001"}, ""), false},
	}
	for _, c := range cases {
		if got := isProcessRunning(running, c.p); got != c.want {
			t.Errorf("%s: isProcessRunning = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWaitEnvdAndListProcessesRetriesUntilEnvdAnswers(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/process.Process/List" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("X-Access-Token") != "sbx-id" {
			t.Errorf("missing access token")
		}
		if calls.Add(1) < 3 { // envd still starting
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"processes":[{"config":{"cmd":"/bin/bash","args":["-l","-c","python -m http.server 8001"]},"pid":20}]}`))
	}))
	defer srv.Close()

	dest, _ := url.Parse(srv.URL)
	running, err := waitEnvdAndListProcesses(dest, "sbx-id")
	if err != nil {
		t.Fatalf("waitEnvdAndListProcesses: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("list calls = %d, want 3", calls.Load())
	}
	if len(running) != 1 || running[0].PID != 20 {
		t.Errorf("unexpected processes: %+v", running)
	}
}

func TestDescribeProcessOmitsEnvs(t *testing.T) {
	p := proc("/bin/bash", []string{"-c", "run"}, "")
	p.Config.Envs = map[string]string{"OPENAI_API_KEY": "sk-secret"}
	if got := describeProcess(p); strings.Contains(got, "sk-secret") || strings.Contains(got, "OPENAI_API_KEY") {
		t.Errorf("describeProcess leaks envs: %s", got)
	}
}
