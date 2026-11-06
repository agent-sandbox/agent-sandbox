package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	fakersinformer "knative.dev/pkg/client/injection/kube/informers/apps/v1/replicaset/fake"
	fakepodinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod/fake"
	reconcilertesting "knative.dev/pkg/reconciler/testing"

	_ "knative.dev/pkg/client/injection/kube/client/fake"
)

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func TestParseLabels(t *testing.T) {
	many := make([]string, MaxUserLabels+1)
	for i := range many {
		many[i] = fmt.Sprintf("k%d=v", i)
	}
	exactlyMax := strings.Join(many[:MaxUserLabels], ",")

	cases := []struct {
		name    string
		in      string
		want    map[string]string
		wantErr bool
	}{
		{name: "empty", in: "", want: map[string]string{}},
		{name: "blank", in: "   ", want: map[string]string{}},
		{name: "single", in: "team=a", want: map[string]string{"team": "a"}},
		{name: "multiple", in: "team=a,count=2,user.alice=true", want: map[string]string{"team": "a", "count": "2", "user.alice": "true"}},
		{name: "spaces are trimmed", in: " team = a , count=2 ", want: map[string]string{"team": "a", "count": "2"}},
		{name: "empty value allowed", in: "flag=", want: map[string]string{"flag": ""}},
		{name: "dots dashes underscores", in: "a.b-c_d=x.y-z_1", want: map[string]string{"a.b-c_d": "x.y-z_1"}},
		{name: "63 char key and value", in: strings.Repeat("k", 63) + "=" + strings.Repeat("v", 63),
			want: map[string]string{strings.Repeat("k", 63): strings.Repeat("v", 63)}},
		{name: "exactly max labels", in: exactlyMax, want: nil /* checked by length below */},

		{name: "missing equals", in: "team", wantErr: true},
		{name: "empty key", in: "=a", wantErr: true},
		{name: "empty pair", in: "team=a,,count=2", wantErr: true},
		{name: "trailing comma", in: "team=a,", wantErr: true},
		{name: "old colon syntax", in: "team:a", wantErr: true},
		{name: "key with slash", in: "sbx.label/x=a", wantErr: true},
		{name: "key with at", in: "alice@example.com=true", wantErr: true},
		{name: "key starts with dash", in: "-team=a", wantErr: true},
		{name: "key ends with dot", in: "team.=a", wantErr: true},
		{name: "key too long", in: strings.Repeat("k", 64) + "=a", wantErr: true},
		{name: "value with space", in: "team=a b", wantErr: true},
		{name: "value with equals", in: "team=a=b", wantErr: true},
		{name: "value too long", in: "team=" + strings.Repeat("v", 64), wantErr: true},
		{name: "non ascii", in: "团队=a", wantErr: true},
		{name: "duplicate key", in: "team=a,team=b", wantErr: true},
		{name: "too many labels", in: strings.Join(many, ","), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseLabels(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseLabels(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLabels(%q) error: %v", tc.in, err)
			}
			if tc.want == nil {
				if len(got) != MaxUserLabels {
					t.Fatalf("got %d labels, want %d", len(got), MaxUserLabels)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseLabels(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatLabels(t *testing.T) {
	if got := FormatLabels(nil); got != "" {
		t.Errorf("FormatLabels(nil) = %q, want empty", got)
	}
	m := map[string]string{"team": "a", "count": "2", "flag": "", "b.x": "1"}
	got := FormatLabels(m)
	if want := "b.x=1,count=2,flag=,team=a"; got != want {
		t.Errorf("FormatLabels = %q, want %q (sorted by key)", got, want)
	}
	back, err := ParseLabels(got)
	if err != nil {
		t.Fatalf("round trip parse error: %v", err)
	}
	if !reflect.DeepEqual(back, m) {
		t.Errorf("round trip = %v, want %v", back, m)
	}
}

func TestExtractUserLabels(t *testing.T) {
	got := ExtractUserLabels(map[string]string{
		"owner":                  "agent-sandbox",
		UserLabel:                "user-a",
		PoolLabel:                "false",
		UserLabelPrefix + "team": "a",
		UserLabelPrefix + "n":    "2",
		"other.io/team":          "ignored",
	})
	want := map[string]string{"team": "a", "n": "2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractUserLabels = %v, want %v", got, want)
	}
	if got := ExtractUserLabels(nil); len(got) != 0 {
		t.Errorf("ExtractUserLabels(nil) = %v, want empty", got)
	}
}

func TestSandboxUserLabelsFromSandboxData(t *testing.T) {
	if got := (&Sandbox{}).UserLabels(); got == nil || len(got) != 0 {
		t.Errorf("no metadata: got %v, want empty non-nil map", got)
	}
	// sandbox-data (metadata) is the source of truth; ReplicaSet labels are ignored.
	sb := &Sandbox{
		Metadata: map[string]string{MetadataLabels: "team=a"},
		ReplicaSet: &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			UserLabelPrefix + "team": "stale", UserLabelPrefix + "extra": "x",
		}}},
	}
	if got := sb.UserLabels(); !reflect.DeepEqual(got, map[string]string{"team": "a"}) {
		t.Errorf("UserLabels = %v, want from metadata only", got)
	}
}

func TestReplaceUserLabels(t *testing.T) {
	dst := map[string]string{
		"owner":                  "agent-sandbox",
		UserLabel:                "user-a",
		UserLabelPrefix + "old":  "x",
		UserLabelPrefix + "team": "a",
	}
	got := replaceUserLabels(dst, map[string]string{"team": "b", "count": "3"})
	want := map[string]string{
		"owner":                   "agent-sandbox",
		UserLabel:                 "user-a",
		UserLabelPrefix + "team":  "b",
		UserLabelPrefix + "count": "3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("full replace = %v, want %v", got, want)
	}

	cleared := replaceUserLabels(got, map[string]string{})
	if !reflect.DeepEqual(cleared, map[string]string{"owner": "agent-sandbox", UserLabel: "user-a"}) {
		t.Errorf("empty set should clear user labels only, got %v", cleared)
	}

	fresh := replaceUserLabels(nil, map[string]string{"a": "1"})
	if !reflect.DeepEqual(fresh, map[string]string{UserLabelPrefix + "a": "1"}) {
		t.Errorf("nil dst = %v", fresh)
	}
}

func TestApplyUserLabelsToReplicaSet(t *testing.T) {
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		"sandbox": "sb-1", UserLabelPrefix + "old": "x",
	}}}
	rs.Spec.Template.Labels = map[string]string{"sandbox": "sb-1", UserLabelPrefix + "old": "x"}

	applyUserLabelsToReplicaSet(rs, map[string]string{"team": "a"})

	want := map[string]string{"sandbox": "sb-1", UserLabelPrefix + "team": "a"}
	if !reflect.DeepEqual(rs.Labels, want) {
		t.Errorf("ReplicaSet labels = %v, want %v", rs.Labels, want)
	}
	if !reflect.DeepEqual(rs.Spec.Template.Labels, want) {
		t.Errorf("pod template labels = %v, want %v", rs.Spec.Template.Labels, want)
	}

	// A ReplicaSet without any labels gets them allocated.
	empty := &appsv1.ReplicaSet{}
	applyUserLabelsToReplicaSet(empty, map[string]string{"team": "a"})
	if empty.Labels[UserLabelPrefix+"team"] != "a" || empty.Spec.Template.Labels[UserLabelPrefix+"team"] != "a" {
		t.Errorf("labels not allocated: %v / %v", empty.Labels, empty.Spec.Template.Labels)
	}
}

func TestBuildListSelector(t *testing.T) {
	// k8s label set of a sandbox owned by user-a.
	set := func(user string, userLabels map[string]string) labels.Set {
		s := labels.Set{"owner": "agent-sandbox", UserLabel: user}
		for k, v := range userLabels {
			s[UserLabelPrefix+k] = v
		}
		return s
	}
	mine := set("user-a", map[string]string{"team": "a", "count": "2", "locked": "true"})
	other := set("user-b", map[string]string{"team": "a", "count": "2", "locked": "true"})

	cases := []struct {
		selector string
		match    bool
	}{
		{"", true},
		{"   ", true},
		{"team=a", true},
		{"team==a", true},
		{"team=b", false},
		{"team!=b", true},
		{"team in (a,b)", true},
		{"team notin (a,b)", false},
		{"count<3", true},
		{"count<2", false},
		{"count>1", true},
		{"count>2", false},
		{"locked", true},
		{"!locked", false},
		{"!missing", true},
		{"missing", false},
		{"team=a,count<3,locked", true},
		{"team=a,count<3,!locked", false},
		// System label names are user label keys here, so they cannot match system labels.
		{"owner=agent-sandbox", false},
		{"sbx-user=user-a", false},
	}
	for _, tc := range cases {
		t.Run(tc.selector, func(t *testing.T) {
			sel, err := BuildListSelector("user-a", tc.selector)
			if err != nil {
				t.Fatalf("BuildListSelector(%q) error: %v", tc.selector, err)
			}
			if got := sel.Matches(mine); got != tc.match {
				t.Errorf("selector %q (%s) matches own sandbox = %v, want %v", tc.selector, sel, got, tc.match)
			}
			if sel.Matches(other) {
				t.Errorf("selector %q (%s) matched another user's sandbox", tc.selector, sel)
			}
		})
	}
}

func TestBuildListSelectorCannotWidenScope(t *testing.T) {
	other := labels.Set{UserLabel: "user-b", UserLabelPrefix + "team": "a"}
	for _, s := range []string{"sbx-user=user-b", "sbx-user in (user-a,user-b)", "!sbx-user", "sbx-user"} {
		sel, err := BuildListSelector("user-a", s)
		if err != nil {
			continue // rejecting is also safe
		}
		if sel.Matches(other) {
			t.Errorf("selector %q widened scope to user-b: %s", s, sel)
		}
	}
}

func TestBuildListSelectorErrors(t *testing.T) {
	for _, s := range []string{
		"sbx.label/team=a", // explicit prefix
		"a/b=c",            // any slash
		"team=a b",         // invalid value
		"count<abc",        // non-integer comparison
		"team in (a",       // syntax
		"=a",
		"-team=a",
		strings.Repeat("k", 64) + "=a",
	} {
		if sel, err := BuildListSelector("user-a", s); err == nil {
			t.Errorf("BuildListSelector(%q) = %s, want error", s, sel)
		}
	}
}

func TestMergeLabels(t *testing.T) {
	t.Run("sandbox wins over template, result stays in metadata in canonical form", func(t *testing.T) {
		sb := &Sandbox{Metadata: map[string]string{MetadataLabels: " team=a , count=1", "foo": "bar"}}
		if err := sb.mergeLabels("team=tpl,env=prod"); err != nil {
			t.Fatal(err)
		}
		if got := sb.Metadata[MetadataLabels]; got != "count=1,env=prod,team=a" {
			t.Errorf("metadata labels = %q, want %q", got, "count=1,env=prod,team=a")
		}
		if want := map[string]string{"team": "a", "count": "1", "env": "prod"}; !reflect.DeepEqual(sb.UserLabels(), want) {
			t.Errorf("UserLabels = %v, want %v", sb.UserLabels(), want)
		}
		if sb.Metadata["foo"] != "bar" {
			t.Errorf("other metadata lost: %v", sb.Metadata)
		}
	})
	t.Run("no labels anywhere", func(t *testing.T) {
		sb := &Sandbox{Metadata: map[string]string{MetadataLabels: ""}}
		if err := sb.mergeLabels(""); err != nil {
			t.Fatal(err)
		}
		if _, ok := sb.Metadata[MetadataLabels]; ok {
			t.Errorf("empty labels kept in metadata: %v", sb.Metadata)
		}
		if got := sb.UserLabels(); got == nil || len(got) != 0 {
			t.Errorf("UserLabels = %v, want empty non-nil", got)
		}
	})
	t.Run("invalid sandbox labels", func(t *testing.T) {
		sb := &Sandbox{Metadata: map[string]string{MetadataLabels: "bad key=a"}}
		if err := sb.mergeLabels(""); err == nil {
			t.Error("want error")
		}
	})
	t.Run("invalid template labels", func(t *testing.T) {
		sb := &Sandbox{Metadata: map[string]string{}}
		if err := sb.mergeLabels("bad"); err == nil || !strings.Contains(err.Error(), "template") {
			t.Errorf("want template error, got %v", err)
		}
	})
	t.Run("combined count over max", func(t *testing.T) {
		own, tpl := make([]string, 20), make([]string, 20)
		for i := range own {
			own[i] = fmt.Sprintf("a%d=v", i)
			tpl[i] = fmt.Sprintf("b%d=v", i)
		}
		sb := &Sandbox{Metadata: map[string]string{MetadataLabels: strings.Join(own, ",")}}
		if err := sb.mergeLabels(strings.Join(tpl, ",")); err == nil {
			t.Error("want too many labels error")
		}
	})
}

// ---------------------------------------------------------------------------
// Make and buildReplicaSet
// ---------------------------------------------------------------------------

func withTemplates(t *testing.T, tpls ...*config.Template) {
	t.Helper()
	prev := config.Templates
	config.Templates = tpls
	t.Cleanup(func() { config.Templates = prev })
}

func TestMakeMergesLabelsInMetadata(t *testing.T) {
	withTemplates(t, &config.Template{
		Name: "tpl", Image: "img",
		Metadata: map[string]string{MetadataLabels: "team=tpl,env=prod", "foo": "bar"},
	})

	sb := GetDefaultSandbox()
	sb.Template = "tpl"
	sb.Metadata = map[string]string{MetadataLabels: "team=a"}
	if err := sb.Make(); err != nil {
		t.Fatal(err)
	}
	// Merged per key, not taken as a whole from the template.
	if got := sb.Metadata[MetadataLabels]; got != "env=prod,team=a" {
		t.Errorf("metadata labels = %q, want %q", got, "env=prod,team=a")
	}
	if sb.Metadata["foo"] != "bar" {
		t.Errorf("other template metadata not merged: %v", sb.Metadata)
	}
}

func TestMakeRejectsInvalidLabels(t *testing.T) {
	withTemplates(t, &config.Template{Name: "tpl", Image: "img"})
	sb := GetDefaultSandbox()
	sb.Template = "tpl"
	sb.Metadata = map[string]string{MetadataLabels: "team:a"}
	if err := sb.Make(); err == nil {
		t.Error("Make with invalid labels: want error")
	}
}

func TestMetadataLabelsInvalid(t *testing.T) {
	// Not reachable after Make; an invalid value yields no labels instead of failing.
	sb := &Sandbox{Metadata: map[string]string{MetadataLabels: "bad key=a"}}
	if got := sb.UserLabels(); len(got) != 0 {
		t.Errorf("UserLabels = %v, want empty", got)
	}
	if got := (&Sandbox{}).UserLabels(); got == nil || len(got) != 0 {
		t.Errorf("nil metadata: %v", got)
	}
}

func TestSetMetadataLabels(t *testing.T) {
	m := map[string]string{"foo": "bar"}
	setMetadataLabels(m, map[string]string{"team": "a", "count": "2"})
	if m[MetadataLabels] != "count=2,team=a" || m["foo"] != "bar" {
		t.Errorf("set = %v", m)
	}
	setMetadataLabels(m, nil)
	if _, ok := m[MetadataLabels]; ok || m["foo"] != "bar" {
		t.Errorf("clear = %v", m)
	}
}

func TestBuildReplicaSetAppliesLabels(t *testing.T) {
	blueprint, err := os.ReadFile("../../config/sandbox.yaml")
	if err != nil {
		t.Fatal(err)
	}
	prevBP, prevCfg := config.SandboxBlueprint, config.Cfg
	config.SandboxBlueprint = string(blueprint)
	config.Cfg = &config.Config{SandboxNamespace: "default"}
	t.Cleanup(func() { config.SandboxBlueprint, config.Cfg = prevBP, prevCfg })
	withTemplates(t, &config.Template{Name: "tpl", Image: "img"})

	sb := GetDefaultSandbox()
	sb.User = "user-a"
	sb.Template = "tpl"
	sb.Metadata = map[string]string{MetadataLabels: "team=a,count=0"}
	if err := sb.Make(); err != nil {
		t.Fatal(err)
	}

	rs, err := buildReplicaSet(sb)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]map[string]string{"replicaset": rs.Labels, "pod template": rs.Spec.Template.Labels} {
		if got[UserLabelPrefix+"team"] != "a" || got[UserLabelPrefix+"count"] != "0" {
			t.Errorf("%s labels missing user labels: %v", name, got)
		}
		if got[UserLabel] != "user-a" || got["sandbox"] != sb.Name {
			t.Errorf("%s system labels lost: %v", name, got)
		}
	}
	// sandbox-data holds the latest configuration, labels in metadata.
	data := &Sandbox{}
	if err := json.Unmarshal([]byte(rs.Annotations[AnnotationSandboxData]), data); err != nil {
		t.Fatalf("sandbox-data: %v", err)
	}
	if got := data.Metadata[MetadataLabels]; got != "count=0,team=a" {
		t.Errorf("sandbox-data metadata labels = %q", got)
	}
	// Create has no previous configuration.
	if _, ok := rs.Annotations[AnnotationLastAppliedConfiguration]; ok {
		t.Errorf("last-applied set on create: %v", rs.Annotations)
	}
	// The selector stays on the immutable system label only.
	if !reflect.DeepEqual(rs.Spec.Selector.MatchLabels, map[string]string{"sandbox": sb.Name}) {
		t.Errorf("selector changed: %v", rs.Spec.Selector.MatchLabels)
	}
}

// ---------------------------------------------------------------------------
// Controller paths backed by fake K8s client and informers
// ---------------------------------------------------------------------------

type fakeEnv struct {
	ctx  context.Context
	c    *Controller
	pm   *PoolManager
	rsIx interface{ Add(obj interface{}) error }
	pdIx interface{ Add(obj interface{}) error }
}

func setupFakeEnv(t *testing.T) *fakeEnv {
	t.Helper()
	prev := config.Cfg
	config.Cfg = &config.Config{SandboxNamespace: "default"}
	t.Cleanup(func() { config.Cfg = prev })

	ctx, _ := reconcilertesting.SetupFakeContext(t)
	c := NewController(ctx, nil, nil, nil)
	return &fakeEnv{
		ctx:  ctx,
		c:    c,
		pm:   &PoolManager{client: c.kclient, rootCtx: ctx},
		rsIx: fakersinformer.Get(ctx).Informer().GetIndexer(),
		pdIx: fakepodinformer.Get(ctx).Informer().GetIndexer(),
	}
}

func newLabeledRS(name, user string, userLabels map[string]string) *appsv1.ReplicaSet {
	sysLabels := map[string]string{"owner": "agent-sandbox", "sandbox": name, UserLabel: user, PoolLabel: "false"}
	// sandbox-data carries the labels in metadata too, as written on create.
	data := map[string]any{"name": name}
	if len(userLabels) > 0 {
		data["metadata"] = map[string]string{MetadataLabels: FormatLabels(userLabels)}
	}
	raw, _ := json.Marshal(data)
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "default",
		Labels:      replaceUserLabels(copyMap(sysLabels), userLabels),
		Annotations: map[string]string{AnnotationSandboxData: string(raw)},
	}}
	rs.Spec.Template.Labels = replaceUserLabels(copyMap(sysLabels), userLabels)
	return rs
}

func newPod(name, sandboxName string, extra map[string]string) *corev1.Pod {
	l := map[string]string{"sandbox": sandboxName}
	for k, v := range extra {
		l[k] = v
	}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: l}}
}

// addRS stores rs in both the fake API server and the informer cache.
func (e *fakeEnv) addRS(t *testing.T, rs *appsv1.ReplicaSet) {
	t.Helper()
	if _, err := e.c.kclient.AppsV1().ReplicaSets("default").Create(context.TODO(), rs, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := e.rsIx.Add(rs); err != nil {
		t.Fatal(err)
	}
}

func (e *fakeEnv) addPod(t *testing.T, pod *corev1.Pod) {
	t.Helper()
	if _, err := e.c.kclient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := e.pdIx.Add(pod); err != nil {
		t.Fatal(err)
	}
}

func (e *fakeEnv) getRS(t *testing.T, name string) *appsv1.ReplicaSet {
	t.Helper()
	rs, err := e.c.kclient.AppsV1().ReplicaSets("default").Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func (e *fakeEnv) getPod(t *testing.T, name string) *corev1.Pod {
	t.Helper()
	pod, err := e.c.kclient.CoreV1().Pods("default").Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return pod
}

func names(sbs []*Sandbox) []string {
	out := make([]string, 0, len(sbs))
	for _, sb := range sbs {
		out = append(out, sb.ReplicaSet.Name)
	}
	sort.Strings(out)
	return out
}

func TestControllerListByLabels(t *testing.T) {
	e := setupFakeEnv(t)
	e.addRS(t, newLabeledRS("a-1", "user-a", map[string]string{"team": "a", "count": "1"}))
	e.addRS(t, newLabeledRS("a-2", "user-a", map[string]string{"team": "a", "count": "3", "locked": "true"}))
	e.addRS(t, newLabeledRS("a-3", "user-a", nil))
	e.addRS(t, newLabeledRS("b-1", "user-b", map[string]string{"team": "a", "count": "1"}))

	cases := []struct {
		selector string
		want     []string
	}{
		{"", []string{"a-1", "a-2", "a-3"}},
		{"team=a", []string{"a-1", "a-2"}},
		{"team=a,count<3", []string{"a-1"}},
		{"!locked", []string{"a-1", "a-3"}},
		{"team", []string{"a-1", "a-2"}},
		{"!team", []string{"a-3"}},
		{"count in (1,3)", []string{"a-1", "a-2"}},
		{"team=zzz", []string{}},
		{"sbx-user=user-b", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.selector, func(t *testing.T) {
			sbs, err := e.c.List("user-a", tc.selector)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(sbs); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("List(%q) = %v, want %v", tc.selector, got, tc.want)
			}
		})
	}
}

func TestControllerListInvalidSelector(t *testing.T) {
	e := setupFakeEnv(t)
	_, err := e.c.List("user-a", "count<abc")
	if !errors.Is(err, ErrInvalidLabelSelector) {
		t.Errorf("err = %v, want ErrInvalidLabelSelector", err)
	}
}

func TestUpdateUserLabels(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"team": "a", "old": "x"})
	e.addRS(t, rs)
	e.addPod(t, newPod("sb-1-pod", "sb-1", map[string]string{UserLabel: "user-a", UserLabelPrefix + "old": "x"}))
	e.addPod(t, newPod("other-pod", "sb-2", map[string]string{UserLabelPrefix + "old": "x"}))

	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"
	if err := e.c.UpdateUserLabels(sb, map[string]string{"team": "b", "count": "3"}, nil); err != nil {
		t.Fatal(err)
	}

	got := e.getRS(t, "sb-1")
	wantUser := map[string]string{"team": "b", "count": "3"}
	if u := ExtractUserLabels(got.Labels); !reflect.DeepEqual(u, wantUser) {
		t.Errorf("ReplicaSet user labels = %v, want %v (full replace)", u, wantUser)
	}
	if u := ExtractUserLabels(got.Spec.Template.Labels); !reflect.DeepEqual(u, wantUser) {
		t.Errorf("pod template user labels = %v, want %v", u, wantUser)
	}
	if got.Labels[UserLabel] != "user-a" || got.Labels["owner"] != "agent-sandbox" || got.Labels["sandbox"] != "sb-1" {
		t.Errorf("system labels changed: %v", got.Labels)
	}
	if got.Annotations[AnnotationSandboxData] == "" {
		t.Errorf("annotations lost: %v", got.Annotations)
	}

	// sb.ReplicaSet is refreshed, so a following update of the same object (e.g. the snapshot) does not conflict.
	if !reflect.DeepEqual(ExtractUserLabels(sb.ReplicaSet.Labels), wantUser) {
		t.Errorf("sb.ReplicaSet not refreshed: %v", sb.ReplicaSet.Labels)
	}
	if !reflect.DeepEqual(sb.UserLabels(), wantUser) {
		t.Errorf("sb metadata labels = %v, want %v", sb.UserLabels(), wantUser)
	}
	// Previous configuration moves to last-applied, sandbox-data holds the latest.
	assertLastApplied(t, got, `{"metadata":{"labels":"old=x,team=a"},"name":"sb-1"}`)
	assertSandboxData(t, got, `{"metadata":{"labels":"count=3,team=b"},"name":"sb-1"}`)
	assertLastApplied(t, sb.ReplicaSet, `{"metadata":{"labels":"old=x,team=a"},"name":"sb-1"}`)
	if got := sb.Metadata[MetadataLabels]; got != "count=3,team=b" {
		t.Errorf("sb metadata labels = %q", got)
	}

	pod := e.getPod(t, "sb-1-pod")
	if u := ExtractUserLabels(pod.Labels); !reflect.DeepEqual(u, wantUser) {
		t.Errorf("pod user labels = %v, want %v", u, wantUser)
	}
	if pod.Labels[UserLabel] != "user-a" || pod.Labels["sandbox"] != "sb-1" {
		t.Errorf("pod system labels changed: %v", pod.Labels)
	}
	if other := e.getPod(t, "other-pod"); other.Labels[UserLabelPrefix+"old"] != "x" {
		t.Errorf("pod of another sandbox touched: %v", other.Labels)
	}
}

func TestUpdateUserLabelsClearAll(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"team": "a"})
	e.addRS(t, rs)

	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"
	if err := e.c.UpdateUserLabels(sb, map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	got := e.getRS(t, "sb-1")
	if u := ExtractUserLabels(got.Labels); len(u) != 0 {
		t.Errorf("labels not cleared: %v", u)
	}
	if u := ExtractUserLabels(got.Spec.Template.Labels); len(u) != 0 {
		t.Errorf("template labels not cleared: %v", u)
	}
	assertLastApplied(t, got, `{"metadata":{"labels":"team=a"},"name":"sb-1"}`)
	assertSandboxData(t, got, `{"name":"sb-1"}`)
}

func TestUpdateUserLabelsUsesLatestReplicaSet(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", nil)
	e.addRS(t, rs)

	// Someone else changes the ReplicaSet after the caller read it.
	latest := e.getRS(t, "sb-1")
	latest.Annotations[AnnotationProcessSnapshot] = "snap"
	if _, err := e.c.kclient.AppsV1().ReplicaSets("default").Update(context.TODO(), latest, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	sb := &Sandbox{ReplicaSet: rs.DeepCopy()} // stale copy
	sb.Name = "sb-1"
	if err := e.c.UpdateUserLabels(sb, map[string]string{"team": "a"}, nil); err != nil {
		t.Fatal(err)
	}
	got := e.getRS(t, "sb-1")
	if got.Annotations[AnnotationProcessSnapshot] != "snap" {
		t.Errorf("concurrent change overwritten: %v", got.Annotations)
	}
	if got.Labels[UserLabelPrefix+"team"] != "a" {
		t.Errorf("labels not applied: %v", got.Labels)
	}
}

func TestUpdateUserLabelsMissingSandbox(t *testing.T) {
	e := setupFakeEnv(t)
	sb := &Sandbox{}
	sb.Name = "missing"
	if err := e.c.UpdateUserLabels(sb, map[string]string{"team": "a"}, nil); err == nil {
		t.Error("want error for missing ReplicaSet")
	}
}

func mustSelector(t *testing.T, raw string) labels.Selector {
	t.Helper()
	sel, err := ParseUserSelector(raw)
	if err != nil {
		t.Fatal(err)
	}
	return sel
}

func TestUpdateUserLabelsConditionsMet(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"count": "2", "agentid": "abc"})
	e.addRS(t, rs)
	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"

	if err := e.c.UpdateUserLabels(sb, map[string]string{"team": "a", "count": "3"}, mustSelector(t, "count=2,agentid=abc")); err != nil {
		t.Fatal(err)
	}
	got := e.getRS(t, "sb-1")
	if u := ExtractUserLabels(got.Labels); !reflect.DeepEqual(u, map[string]string{"team": "a", "count": "3"}) {
		t.Errorf("ReplicaSet user labels = %v", u)
	}
	assertLastApplied(t, got, `{"metadata":{"labels":"agentid=abc,count=2"},"name":"sb-1"}`)
}

func TestUpdateUserLabelsConditionsNotMet(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"count": "1"})
	e.addRS(t, rs)
	before := e.getRS(t, "sb-1").ResourceVersion
	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"

	for _, cond := range []string{"count=2", "count>1", "locked", "count=1,agentid=abc"} {
		err := e.c.UpdateUserLabels(sb, map[string]string{"team": "a"}, mustSelector(t, cond))
		if !errors.Is(err, ErrConditionsNotMet) {
			t.Errorf("conditions %q: err = %v, want ErrConditionsNotMet", cond, err)
		}
	}
	got := e.getRS(t, "sb-1")
	if got.ResourceVersion != before || got.Annotations[AnnotationLastAppliedConfiguration] != "" {
		t.Errorf("ReplicaSet written although conditions failed: rv %s -> %s", before, got.ResourceVersion)
	}
	if u := ExtractUserLabels(got.Labels); !reflect.DeepEqual(u, map[string]string{"count": "1"}) {
		t.Errorf("labels changed: %v", u)
	}
}

// Conditions are evaluated on sandbox-data, the source of truth, not on ReplicaSet labels.
func TestUpdateUserLabelsConditionsUseSandboxData(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"count": "1"})
	rs.Labels[UserLabelPrefix+"count"] = "2" // drifted index
	e.addRS(t, rs)
	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"

	if err := e.c.UpdateUserLabels(sb, nil, mustSelector(t, "count=2")); !errors.Is(err, ErrConditionsNotMet) {
		t.Errorf("err = %v, want ErrConditionsNotMet", err)
	}
	if err := e.c.UpdateUserLabels(sb, nil, mustSelector(t, "count=1")); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// With no labels in the request only the conditions are checked; nothing is written.
func TestUpdateUserLabelsCheckOnly(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"count": "2"})
	e.addRS(t, rs)
	before := e.getRS(t, "sb-1").ResourceVersion
	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"

	if err := e.c.UpdateUserLabels(sb, nil, mustSelector(t, "count<3")); err != nil {
		t.Fatal(err)
	}
	if got := e.getRS(t, "sb-1"); got.ResourceVersion != before {
		t.Errorf("check-only wrote the ReplicaSet: rv %s -> %s", before, got.ResourceVersion)
	}
	if sb.ReplicaSet.ResourceVersion != before {
		t.Errorf("sb.ReplicaSet not refreshed")
	}
}

func TestUpdatePoolPodLabels(t *testing.T) {
	e := setupFakeEnv(t)
	e.addPod(t, newPod("pool-pod", "pool-1", map[string]string{
		UserLabel: "", PoolLabel: "true", UserLabelPrefix + "tpl": "x",
	}))

	if err := e.pm.updatePoolPodLabels("pool-1", "user-a", map[string]string{"team": "a"}); err != nil {
		t.Fatal(err)
	}
	pod := e.getPod(t, "pool-pod")
	want := map[string]string{"sandbox": "pool-1", UserLabel: "user-a", PoolLabel: "false", UserLabelPrefix + "team": "a"}
	if !reflect.DeepEqual(pod.Labels, want) {
		t.Errorf("pool pod labels = %v, want %v", pod.Labels, want)
	}
}

func TestAdaptPoolReplicaSetAppliesLabels(t *testing.T) {
	e := setupFakeEnv(t)
	pool := newLabeledRS("pool-1", "", map[string]string{"tpl": "default"})
	pool.Labels[PoolLabel] = "true"
	pool.Labels[IDLabel] = "pool-id"
	e.addRS(t, pool)
	e.addPod(t, newPod("pool-1-pod", "pool-1", map[string]string{PoolLabel: "true", UserLabelPrefix + "tpl": "default"}))

	sb := &Sandbox{Metadata: map[string]string{MetadataLabels: "team=a"}}
	sb.User = "user-a"
	got, err := e.pm.adaptReplicasetToSandbox(pool, sb)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"team": "a"}
	if u := ExtractUserLabels(got.Labels); !reflect.DeepEqual(u, want) {
		t.Errorf("claimed ReplicaSet user labels = %v, want %v", u, want)
	}
	if u := ExtractUserLabels(got.Spec.Template.Labels); !reflect.DeepEqual(u, want) {
		t.Errorf("claimed pod template user labels = %v, want %v", u, want)
	}
	// Claiming is part of create: sandbox-data is the new sandbox, there is no previous configuration.
	if _, ok := got.Annotations[AnnotationLastAppliedConfiguration]; ok {
		t.Errorf("last-applied set on pool claim: %v", got.Annotations)
	}
	data := &Sandbox{}
	if err := json.Unmarshal([]byte(got.Annotations[AnnotationSandboxData]), data); err != nil || data.Metadata[MetadataLabels] != "team=a" {
		t.Errorf("sandbox-data metadata = %v, err %v", data.Metadata, err)
	}
	if got.Labels[UserLabel] != "user-a" || got.Labels[PoolLabel] != "false" {
		t.Errorf("claim labels not set: %v", got.Labels)
	}
	stored := e.getRS(t, "pool-1")
	if u := ExtractUserLabels(stored.Labels); !reflect.DeepEqual(u, want) {
		t.Errorf("stored ReplicaSet user labels = %v, want %v", u, want)
	}

	// Pod labels are synced asynchronously; wait for it (also keeps the goroutine inside the test).
	err = wait.PollUntilContextTimeout(context.TODO(), 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		pod := e.getPod(t, "pool-1-pod")
		return pod.Labels[UserLabel] == "user-a" && reflect.DeepEqual(ExtractUserLabels(pod.Labels), want), nil
	})
	if err != nil {
		t.Errorf("pool pod labels not synced: %v", e.getPod(t, "pool-1-pod").Labels)
	}
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// assertLastApplied checks the last-applied-configuration annotation of rs, ignoring key order.
func assertLastApplied(t *testing.T, rs *appsv1.ReplicaSet, want string) {
	t.Helper()
	assertAnnotationJSON(t, rs, AnnotationLastAppliedConfiguration, want)
}

// assertSandboxData checks the sandbox-data annotation of rs, ignoring key order.
func assertSandboxData(t *testing.T, rs *appsv1.ReplicaSet, want string) {
	t.Helper()
	assertAnnotationJSON(t, rs, AnnotationSandboxData, want)
}

func assertAnnotationJSON(t *testing.T, rs *appsv1.ReplicaSet, key, want string) {
	t.Helper()
	got, ok := rs.Annotations[key]
	if !ok {
		t.Errorf("missing %s annotation: %v", key, rs.Annotations)
		return
	}
	if !jsonEqual(got, want) {
		t.Errorf("%s =\n %s\nwant\n %s", key, got, want)
	}
}

// The snapshot annotation is patched, so a ReplicaSet changed after sb was read (labels update, then the
// ReplicaSet controller bumping its status) does not make the snapshot fail, and nothing else is lost.
func TestSaveProcessSnapshotAfterLabelsUpdate(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"step": "0"})
	e.addRS(t, rs)
	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"
	if err := e.c.UpdateUserLabels(sb, map[string]string{"step": "1"}, nil); err != nil {
		t.Fatal(err)
	}
	// The ReplicaSet controller observes the new pod template generation.
	latest := e.getRS(t, "sb-1")
	latest.Status.ObservedGeneration++
	if _, err := e.c.kclient.AppsV1().ReplicaSets("default").UpdateStatus(context.Background(), latest, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := e.c.saveProcessSnapshot(sb, `{"processes":[]}`); err != nil {
		t.Fatal(err)
	}
	got := e.getRS(t, "sb-1")
	if got.Annotations[AnnotationProcessSnapshot] != `{"processes":[]}` {
		t.Errorf("snapshot annotation = %q", got.Annotations[AnnotationProcessSnapshot])
	}
	assertSandboxData(t, got, `{"metadata":{"labels":"step=1"},"name":"sb-1"}`)
	assertLastApplied(t, got, `{"metadata":{"labels":"step=0"},"name":"sb-1"}`)
	if u := ExtractUserLabels(got.Labels); !reflect.DeepEqual(u, map[string]string{"step": "1"}) {
		t.Errorf("labels = %v", u)
	}
	if sb.ReplicaSet.Annotations[AnnotationProcessSnapshot] == "" {
		t.Error("sb.ReplicaSet not refreshed")
	}
}
