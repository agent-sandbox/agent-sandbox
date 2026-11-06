package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Invalid default labels are rejected on save, before the templates reach the configmap.
func TestSaveTemplatesConfigRejectsInvalidLabels(t *testing.T) {
	a := &Handler{}
	for _, body := range []string{
		`[{"name":"tpl","image":"img","metadata":{"labels":"a:b"}}]`,
		`[{"name":"ok","image":"img"},{"name":"tpl","image":"img","metadata":{"labels":"bad key=a"}}]`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/config/templates", strings.NewReader(body))
		_, err := a.SaveTemplatesConfig(req)
		if err == nil || !strings.Contains(err.Error(), "invalid labels in template tpl") {
			t.Errorf("body %s: err = %v, want invalid labels error", body, err)
		}
	}
}
