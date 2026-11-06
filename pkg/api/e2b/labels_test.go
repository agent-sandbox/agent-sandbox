package e2b

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/agent-sandbox/agent-sandbox/pkg/api/e2b/api"
	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakersinformer "knative.dev/pkg/client/injection/kube/informers/apps/v1/replicaset/fake"
	reconcilertesting "knative.dev/pkg/reconciler/testing"

	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
)

func TestParseLabelsHeader(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    map[string]string // nil means "labels unchanged"
		wantErr bool
	}{
		{name: "labels", raw: `{"labels":"team=a,count=3"}`, want: map[string]string{"team": "a", "count": "3"}},
		{name: "empty clears all", raw: `{"labels":""}`, want: map[string]string{}},
		{name: "no labels key", raw: `{"other":"x"}`, want: nil},
		{name: "empty object", raw: `{}`, want: nil},
		{name: "not json", raw: `labels=team=a`, wantErr: true},
		{name: "old colon syntax", raw: `{"labels":"team:a"}`, wantErr: true},
		{name: "non-string value", raw: `{"labels":3}`, wantErr: true},
		{name: "json array", raw: `["labels"]`, wantErr: true},
		{name: "invalid label", raw: `{"labels":"bad key=a"}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := parseSnapshotMetadata(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseSnapshotMetadata(%s) = %v, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSnapshotMetadata(%s) error: %v", tc.raw, err)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %v, want nil (labels unchanged)", got)
				}
				return
			}
			if got == nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseSnapshotConditions(t *testing.T) {
	cases := []struct {
		raw     string
		want    string // selector String(), "" means no conditions
		wantErr bool
	}{
		{raw: `{"conditions":"count=2,agentid=abc"}`, want: "agentid=abc,count=2"},
		{raw: `{"conditions":"count<3,!locked"}`, want: "count<3,!locked"},
		{raw: `{"conditions":""}`},
		{raw: `{"labels":"team=a"}`},
		{raw: `{"conditions":"a/b=c"}`, wantErr: true},
		{raw: `{"conditions":"count<x"}`, wantErr: true},
		{raw: `{"labels":"team=a","conditions":"=="}`, wantErr: true},
	}
	for _, tc := range cases {
		_, cond, err := parseSnapshotMetadata(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: want error, got %v", tc.raw, cond)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.raw, err)
			continue
		}
		got := ""
		if cond != nil {
			got = cond.String()
		}
		if got != tc.want {
			t.Errorf("%s: conditions = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestConvertToE2BSandboxLabels(t *testing.T) {
	a := &Handler{}
	// Labels come from sandbox-data (metadata) as stored, never rebuilt from ReplicaSet labels.
	sb := &sandbox.Sandbox{
		Metadata: map[string]string{"foo": "bar", sandbox.MetadataLabels: "count=2,team=a"},
		ReplicaSet: &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			sandbox.UserLabel:                 "user-a",
			sandbox.UserLabelPrefix + "team":  "stale",
			sandbox.UserLabelPrefix + "extra": "x",
		}}},
	}
	sb.Name = "sb-1"

	got := a.convertToE2BSandbox(sb)
	if v := got.Metadata[sandbox.MetadataLabels]; v != "count=2,team=a" {
		t.Errorf("metadata labels = %q, want %q from sandbox-data", v, "count=2,team=a")
	}
	if got.Metadata["foo"] != "bar" || got.Metadata["name"] != "sb-1" {
		t.Errorf("metadata = %v", got.Metadata)
	}
	// The sandbox's own metadata is not mutated.
	if !reflect.DeepEqual(sb.Metadata, map[string]string{"foo": "bar", sandbox.MetadataLabels: "count=2,team=a"}) {
		t.Errorf("sb.Metadata mutated: %v", sb.Metadata)
	}

	// ReplicaSet labels without labels in sandbox-data: none returned.
	noLabels := &sandbox.Sandbox{ReplicaSet: &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		sandbox.UserLabelPrefix + "team": "a",
	}}}}
	if _, ok := a.convertToE2BSandbox(noLabels).Metadata[sandbox.MetadataLabels]; ok {
		t.Error("labels rebuilt from ReplicaSet labels")
	}
}

// ---------------------------------------------------------------------------
// HTTP handlers backed by a fake K8s client and ReplicaSet informer
// ---------------------------------------------------------------------------

func newHandlerWithRS(t *testing.T, rss ...*appsv1.ReplicaSet) (*Handler, context.Context) {
	t.Helper()
	prev := config.Cfg
	config.Cfg = &config.Config{SandboxNamespace: "default"}
	t.Cleanup(func() { config.Cfg = prev })

	ctx, _ := reconcilertesting.SetupFakeContext(t)
	c := sandbox.NewController(ctx, nil, nil, nil)
	indexer := fakersinformer.Get(ctx).Informer().GetIndexer()
	for _, rs := range rss {
		if err := indexer.Add(rs); err != nil {
			t.Fatal(err)
		}
		// Updates read the ReplicaSet from the API server, not the cache.
		if _, err := fakekubeclient.Get(ctx).AppsV1().ReplicaSets(rs.Namespace).Create(ctx, rs, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(ctx, c, nil, nil), ctx
}

// labeledRS builds a sandbox ReplicaSet the way create does: labels in sandbox-data metadata,
// expanded to prefixed ReplicaSet labels.
func labeledRS(name, user string, meta map[string]string, userLabels map[string]string) *appsv1.ReplicaSet {
	stored := map[string]string{}
	for k, v := range meta {
		stored[k] = v
	}
	if len(userLabels) > 0 {
		stored[sandbox.MetadataLabels] = sandbox.FormatLabels(userLabels)
	}
	data, _ := json.Marshal(map[string]any{"id": name + "-id", "name": name, "user": user, "metadata": stored})
	l := map[string]string{
		"owner": "agent-sandbox", "sandbox": name,
		sandbox.IDLabel: name + "-id", sandbox.UserLabel: user, sandbox.PoolLabel: "false",
	}
	for k, v := range userLabels {
		l[sandbox.UserLabelPrefix+k] = v
	}
	return &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "default", Labels: l,
		Annotations: map[string]string{sandbox.AnnotationSandboxData: string(data)},
	}}
}

func doList(t *testing.T, h *Handler, user, metadata string) (int, []*api.Sandbox) {
	t.Helper()
	target := "/e2b/v1/v2/sandboxes"
	if metadata != "" {
		target += "?metadata=" + url.QueryEscape(metadata)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(context.WithValue(req.Context(), "user", user))
	rec := httptest.NewRecorder()
	h.ListSandboxes(rec, req)
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	var out []*api.Sandbox
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return rec.Code, out
}

func sandboxNames(sbs []*api.Sandbox) []string {
	out := []string{}
	for _, s := range sbs {
		out = append(out, s.Metadata["name"])
	}
	sort.Strings(out)
	return out
}

// metadataQuery builds the metadata query value the e2b SDK sends: URL-encoded key=value pairs.
func metadataQuery(pairs map[string]string) string {
	v := url.Values{}
	for k, val := range pairs {
		v.Set(k, val)
	}
	return v.Encode()
}

func TestListSandboxesByLabels(t *testing.T) {
	h, _ := newHandlerWithRS(t,
		labeledRS("a-1", "user-a", map[string]string{"env": "dev"}, map[string]string{"team": "a", "count": "1"}),
		labeledRS("a-2", "user-a", map[string]string{"env": "prod"}, map[string]string{"team": "a", "count": "3", "locked": "true"}),
		labeledRS("a-3", "user-a", nil, nil),
		labeledRS("b-1", "user-b", map[string]string{"env": "dev"}, map[string]string{"team": "a", "count": "1"}),
	)

	cases := []struct {
		name  string
		query map[string]string
		want  []string
	}{
		{name: "no filter", query: nil, want: []string{"a-1", "a-2", "a-3"}},
		{name: "equality", query: map[string]string{"labels": "team=a"}, want: []string{"a-1", "a-2"}},
		{name: "less than", query: map[string]string{"labels": "count<3"}, want: []string{"a-1"}},
		{name: "greater than", query: map[string]string{"labels": "count>1"}, want: []string{"a-2"}},
		{name: "not exists", query: map[string]string{"labels": "!locked"}, want: []string{"a-1", "a-3"}},
		{name: "in", query: map[string]string{"labels": "count in (1,3)"}, want: []string{"a-1", "a-2"}},
		{name: "combined", query: map[string]string{"labels": "team=a,count<3,!locked"}, want: []string{"a-1"}},
		{name: "labels and other metadata", query: map[string]string{"labels": "team=a", "env": "prod"}, want: []string{"a-2"}},
		{name: "other metadata only", query: map[string]string{"env": "dev"}, want: []string{"a-1"}},
		{name: "no match", query: map[string]string{"labels": "team=zzz"}, want: []string{}},
		{name: "cannot reach other user", query: map[string]string{"labels": "sbx-user=user-b"}, want: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, sbs := doList(t, h, "user-a", metadataQuery(tc.query))
			if code != http.StatusOK {
				t.Fatalf("status = %d", code)
			}
			if got := sandboxNames(sbs); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// The Python SDK quotes each key and value, url-encodes the pairs and the HTTP client encodes the query,
// so labels arrive encoded three times. These raw queries are what e2b 2.21.1 sends for
// Sandbox.list(query=SandboxQuery(metadata={"labels": ..., ...})).
func TestListSandboxesPythonSDKEncoding(t *testing.T) {
	h, _ := newHandlerWithRS(t,
		labeledRS("a-1", "user-a", map[string]string{"env": "dev"}, map[string]string{"run": "r1", "user-name": "alice"}),
		labeledRS("a-2", "user-a", nil, map[string]string{"run": "r1", "user-name": "bob", "count": "3"}),
	)
	cases := []struct {
		name, rawQuery string
		want           []string
	}{
		// {"labels": "run=r1,user-name=alice"}
		{"equality", "metadata=labels%3Drun%25253Dr1%25252Cuser-name%25253Dalice", []string{"a-1"}},
		// {"labels": "run=r1,count>2,user-name in (alice,bob)"}
		{"operators and spaces", "metadata=labels%3Drun%25253Dr1%25252Ccount%25253E2%25252Cuser-name%252520in%252520%252528alice%25252Cbob%252529", []string{"a-2"}},
		// {"labels": "run=r1", "env": "dev"}
		{"with other metadata", "metadata=labels%3Drun%25253Dr1%26env%3Ddev", []string{"a-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/e2b/v1/v2/sandboxes?"+tc.rawQuery, nil)
			req = req.WithContext(context.WithValue(req.Context(), "user", "user-a"))
			rec := httptest.NewRecorder()
			h.ListSandboxes(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			var out []*api.Sandbox
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if got := sandboxNames(out); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListSandboxesReturnsLabels(t *testing.T) {
	h, _ := newHandlerWithRS(t, labeledRS("a-1", "user-a", nil, map[string]string{"team": "a", "count": "1"}))
	_, sbs := doList(t, h, "user-a", "")
	if len(sbs) != 1 {
		t.Fatalf("got %d sandboxes", len(sbs))
	}
	if got := sbs[0].Metadata[sandbox.MetadataLabels]; got != "count=1,team=a" {
		t.Errorf("labels = %q, want %q", got, "count=1,team=a")
	}
}

func TestListSandboxesInvalidSelector(t *testing.T) {
	h, _ := newHandlerWithRS(t)
	for _, sel := range []string{"count<abc", "team in (a", "sbx.label/team=a"} {
		code, _ := doList(t, h, "user-a", metadataQuery(map[string]string{"labels": sel}))
		if code != http.StatusBadRequest {
			t.Errorf("selector %q: status = %d, want 400", sel, code)
		}
	}
}

func doSnapshot(t *testing.T, h *Handler, user, id, metadataHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/e2b/v1/sandboxes/%s/snapshots", id), nil)
	req.SetPathValue("sandboxID", id)
	if user != "" {
		req = req.WithContext(context.WithValue(req.Context(), "user", user))
	}
	if metadataHeader != "" {
		req.Header.Set("metadata", metadataHeader)
	}
	rec := httptest.NewRecorder()
	h.SnapshotSandbox(rec, req)
	return rec
}

func TestSnapshotInvalidLabelsRejectedBeforeSnapshot(t *testing.T) {
	rs := labeledRS("a-1", "user-a", nil, map[string]string{"team": "a"})
	h, _ := newHandlerWithRS(t, rs)

	for _, header := range []string{`not json`, `{"labels":"team:b"}`, `{"labels":"bad key=b"}`, `{"labels":1}`} {
		rec := doSnapshot(t, h, "user-a", "a-1-id", header)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("header %s: status = %d, want 400, body=%s", header, rec.Code, rec.Body.String())
		}
	}
	// Labels on the cached ReplicaSet are untouched.
	sb, err := h.controller.GetByID("a-1-id")
	if err != nil {
		t.Fatal(err)
	}
	if got := sb.UserLabels(); !reflect.DeepEqual(got, map[string]string{"team": "a"}) {
		t.Errorf("labels changed by a rejected request: %v", got)
	}
}

func TestSnapshotUnknownSandbox(t *testing.T) {
	h, _ := newHandlerWithRS(t)
	rec := doSnapshot(t, h, "user-a", "missing", `{"labels":"team=a"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// Snapshot writes (labels and the process snapshot) are limited to the sandbox owner.
func TestSnapshotOtherUsersSandbox(t *testing.T) {
	h, _ := newHandlerWithRS(t, labeledRS("a-1", "user-a", nil, map[string]string{"team": "a"}))

	rec := doSnapshot(t, h, "user-b", "a-1-id", `{"labels":""}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("other user: status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "owned") {
		t.Errorf("response reveals the sandbox exists: %s", rec.Body.String())
	}
	sb, err := h.controller.GetByID("a-1-id")
	if err != nil {
		t.Fatal(err)
	}
	if got := sb.UserLabels(); !reflect.DeepEqual(got, map[string]string{"team": "a"}) {
		t.Errorf("labels changed by another user: %v", got)
	}
}

func TestSnapshotWithoutUser(t *testing.T) {
	h, _ := newHandlerWithRS(t, labeledRS("a-1", "user-a", nil, nil))
	if rec := doSnapshot(t, h, "", "a-1-id", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// Unmet conditions return 412 and change nothing; the snapshot is not taken.
func TestSnapshotConditionsNotMet(t *testing.T) {
	h, _ := newHandlerWithRS(t, labeledRS("a-1", "user-a", nil, map[string]string{"count": "1", "agentid": "abc"}))

	for _, header := range []string{
		`{"labels":"team=a,count=3","conditions":"count=2,agentid=abc"}`,
		`{"conditions":"count=2"}`,
		`{"labels":"","conditions":"missing"}`,
	} {
		rec := doSnapshot(t, h, "user-a", "a-1-id", header)
		if rec.Code != http.StatusPreconditionFailed {
			t.Errorf("header %s: status = %d, want 412, body=%s", header, rec.Code, rec.Body.String())
		}
	}
	sb, err := h.controller.GetByID("a-1-id")
	if err != nil {
		t.Fatal(err)
	}
	if got := sb.UserLabels(); !reflect.DeepEqual(got, map[string]string{"count": "1", "agentid": "abc"}) {
		t.Errorf("labels changed by a failed condition: %v", got)
	}
}

func TestSnapshotInvalidConditions(t *testing.T) {
	h, _ := newHandlerWithRS(t, labeledRS("a-1", "user-a", nil, map[string]string{"count": "1"}))
	if rec := doSnapshot(t, h, "user-a", "a-1-id", `{"labels":"team=a","conditions":"count<x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}
