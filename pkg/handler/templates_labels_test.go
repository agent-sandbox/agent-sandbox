package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
)

// Invalid default labels are rejected on save, before the templates reach the configmap.
func TestSaveTemplatesConfigRejectsInvalidLabels(t *testing.T) {
	// The handler is admin-only; IsAdmin requires the configured system token.
	prev := config.Cfg
	config.Cfg = &config.Config{SystemToken: "sys-test"}
	t.Cleanup(func() { config.Cfg = prev })

	a := &Handler{}
	for _, body := range []string{
		`[{"name":"tpl","image":"img","metadata":{"labels":"a:b"}}]`,
		`[{"name":"ok","image":"img"},{"name":"tpl","image":"img","metadata":{"labels":"bad key=a"}}]`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/config/templates", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), "user", "sys-test"))
		_, err := a.SaveTemplatesConfig(req)
		if err == nil || !strings.Contains(err.Error(), "invalid labels in template tpl") {
			t.Errorf("body %s: err = %v, want invalid labels error", body, err)
		}
	}
}
