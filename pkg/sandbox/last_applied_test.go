package sandbox

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func rsWithConfig(sandboxData, lastApplied string) *appsv1.ReplicaSet {
	anns := map[string]string{"other": "kept"}
	if sandboxData != "" {
		anns[AnnotationSandboxData] = sandboxData
	}
	if lastApplied != "" {
		anns[AnnotationLastAppliedConfiguration] = lastApplied
	}
	return &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "sb-1", Annotations: anns}}
}

func TestApplyConfigUpdateRotates(t *testing.T) {
	v1 := `{"name":"sb-1","template":"tpl","timeout":600,"metadata":{"labels":"team=a"}}`
	rs := rsWithConfig(v1, "")

	// v1 -> v2: previous goes to last-applied verbatim, latest to sandbox-data.
	if _, err := applyConfigUpdate(rs, setLabelsConfig(map[string]string{"team": "b"})); err != nil {
		t.Fatal(err)
	}
	if got := rs.Annotations[AnnotationLastAppliedConfiguration]; got != v1 {
		t.Errorf("last-applied = %s, want previous sandbox-data verbatim %s", got, v1)
	}
	assertSandboxData(t, rs, `{"name":"sb-1","template":"tpl","timeout":600,"metadata":{"labels":"team=b"}}`)

	// v2 -> v3: last-applied follows, it always holds exactly one version back.
	v2 := rs.Annotations[AnnotationSandboxData]
	if _, err := applyConfigUpdate(rs, setLabelsConfig(map[string]string{"team": "c"})); err != nil {
		t.Fatal(err)
	}
	if got := rs.Annotations[AnnotationLastAppliedConfiguration]; got != v2 {
		t.Errorf("last-applied = %s, want %s", got, v2)
	}
	assertSandboxData(t, rs, `{"name":"sb-1","template":"tpl","timeout":600,"metadata":{"labels":"team=c"}}`)

	if rs.Annotations["other"] != "kept" {
		t.Errorf("other annotations changed: %v", rs.Annotations)
	}
}

func TestApplyConfigUpdateNoChangeKeepsPrevious(t *testing.T) {
	prev := `{"name":"sb-1","metadata":{"labels":"team=a"}}`
	cur := `{"name":"sb-1","metadata":{"labels":"team=b"}}`
	rs := rsWithConfig(cur, prev)

	// Same labels again: nothing changes, the previous configuration is not overwritten.
	if _, err := applyConfigUpdate(rs, setLabelsConfig(map[string]string{"team": "b"})); err != nil {
		t.Fatal(err)
	}
	if rs.Annotations[AnnotationLastAppliedConfiguration] != prev || rs.Annotations[AnnotationSandboxData] != cur {
		t.Errorf("no-op update changed annotations: %v", rs.Annotations)
	}
}

func TestApplyConfigUpdateKeepsUnknownFieldsAndNumbers(t *testing.T) {
	// Fields Sandbox does not know and large numbers survive the update unchanged.
	rs := rsWithConfig(`{"name":"sb-1","futureField":{"x":[1,2]},"big":12345678901234567890,"timeout":-1}`, "")
	if _, err := applyConfigUpdate(rs, setLabelsConfig(map[string]string{"team": "a"})); err != nil {
		t.Fatal(err)
	}
	got := rs.Annotations[AnnotationSandboxData]
	want := `{"big":12345678901234567890,"futureField":{"x":[1,2]},"metadata":{"labels":"team=a"},"name":"sb-1","timeout":-1}`
	if got != want { // exact text: numbers must not be rewritten as floats
		t.Errorf("sandbox-data =\n %s\nwant\n %s", got, want)
	}
}

func TestApplyConfigUpdateClearLabels(t *testing.T) {
	rs := rsWithConfig(`{"name":"sb-1","metadata":{"labels":"team=a"}}`, "")
	for _, empty := range []map[string]string{{}, nil} {
		if _, err := applyConfigUpdate(rs, setLabelsConfig(empty)); err != nil {
			t.Fatal(err)
		}
		assertSandboxData(t, rs, `{"name":"sb-1"}`) // omitted, like the labels field of Sandbox
	}
	// The second clear was a no-op, so last-applied still holds the labelled version.
	assertLastApplied(t, rs, `{"name":"sb-1","metadata":{"labels":"team=a"}}`)
}

func TestSetLabelsConfigKeepsOtherMetadata(t *testing.T) {
	rs := rsWithConfig(`{"name":"sb-1","metadata":{"idleTimeout":"600","labels":"team=a"}}`, "")
	if _, err := applyConfigUpdate(rs, setLabelsConfig(map[string]string{"team": "b", "count": "1"})); err != nil {
		t.Fatal(err)
	}
	assertSandboxData(t, rs, `{"name":"sb-1","metadata":{"idleTimeout":"600","labels":"count=1,team=b"}}`)

	// Clearing removes only the labels key; metadata with other keys stays.
	if _, err := applyConfigUpdate(rs, setLabelsConfig(nil)); err != nil {
		t.Fatal(err)
	}
	assertSandboxData(t, rs, `{"name":"sb-1","metadata":{"idleTimeout":"600"}}`)

	// Clearing a sandbox without metadata changes nothing.
	plain := rsWithConfig(`{"name":"sb-1"}`, "")
	if _, err := applyConfigUpdate(plain, setLabelsConfig(nil)); err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.Annotations[AnnotationLastAppliedConfiguration]; ok {
		t.Errorf("no-op clear rotated: %v", plain.Annotations)
	}
}

func TestApplyConfigUpdateWithoutSandboxData(t *testing.T) {
	rs := rsWithConfig("", "")
	if _, err := applyConfigUpdate(rs, setLabelsConfig(map[string]string{"team": "a"})); err != nil {
		t.Fatal(err)
	}
	assertSandboxData(t, rs, `{"metadata":{"labels":"team=a"}}`)
	if got, ok := rs.Annotations[AnnotationLastAppliedConfiguration]; ok {
		t.Errorf("last-applied = %q, want none (no previous configuration)", got)
	}
}

func TestApplyConfigUpdateInvalidSandboxData(t *testing.T) {
	for _, raw := range []string{`{bad`, `null`, `[1]`} {
		rs := rsWithConfig(raw, "prev")
		if _, err := applyConfigUpdate(rs, setLabelsConfig(map[string]string{"team": "a"})); err == nil {
			t.Errorf("sandbox-data %s: want error", raw)
		}
		if rs.Annotations[AnnotationSandboxData] != raw || rs.Annotations[AnnotationLastAppliedConfiguration] != "prev" {
			t.Errorf("sandbox-data %s: annotations changed: %v", raw, rs.Annotations)
		}
	}
}

func TestJSONEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{`{"a":1,"b":{"c":2}}`, `{"b":{"c":2},"a":1}`, true},
		{`{"a":1}`, ` { "a" : 1 } `, true},
		{`{"a":1}`, `{"a":2}`, false},
		{`{"a":1}`, `{"a":1,"b":null}`, false},
		{`{bad`, `{bad`, false},
	}
	for _, tc := range cases {
		if got := jsonEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("jsonEqual(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// System changes never touch the configuration annotations: here, recording a resume status.
func TestSystemChangeKeepsConfiguration(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"team": "a"})
	rs.Annotations[AnnotationLastAppliedConfiguration] = `{"name":"sb-1"}`
	e.addRS(t, rs)
	data := rs.Annotations[AnnotationSandboxData]

	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"
	e.c.patchResumeStatus(sb, ResumeStatus{PodReady: true})

	got := e.getRS(t, "sb-1")
	if got.Annotations[AnnotationResumeStatus] == "" {
		t.Fatal("system change not applied")
	}
	if got.Annotations[AnnotationSandboxData] != data || got.Annotations[AnnotationLastAppliedConfiguration] != `{"name":"sb-1"}` {
		t.Errorf("system change touched the configuration: %v", got.Annotations)
	}
}

// ReplicaSet labels are re-derived from sandbox-data: labels set on the ReplicaSet outside of
// sandbox-data do not survive an update.
func TestUpdateUserLabelsDerivesFromSandboxData(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"team": "a"})
	rs.Labels[UserLabelPrefix+"drift"] = "x" // not in sandbox-data
	rs.Annotations[AnnotationSandboxData] = `{"name":"sb-1","metadata":{"idleTimeout":"600","labels":"team=a"}}`
	e.addRS(t, rs)

	sb := &Sandbox{ReplicaSet: rs.DeepCopy(), Metadata: map[string]string{"idleTimeout": "600", MetadataLabels: "team=a"}}
	sb.Name = "sb-1"
	if err := e.c.UpdateUserLabels(sb, map[string]string{"team": "b"}, nil); err != nil {
		t.Fatal(err)
	}
	got := e.getRS(t, "sb-1")
	assertSandboxData(t, got, `{"name":"sb-1","metadata":{"idleTimeout":"600","labels":"team=b"}}`)
	if u := ExtractUserLabels(got.Labels); !reflect.DeepEqual(u, map[string]string{"team": "b"}) {
		t.Errorf("ReplicaSet labels = %v, want derived from sandbox-data only", u)
	}
	if !reflect.DeepEqual(sb.Metadata, map[string]string{"idleTimeout": "600", MetadataLabels: "team=b"}) {
		t.Errorf("sb.Metadata = %v", sb.Metadata)
	}
}

// A corrupt sandbox-data fails the labels update and changes nothing.
func TestUpdateUserLabelsInvalidSandboxData(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"team": "a"})
	rs.Annotations[AnnotationSandboxData] = `{bad`
	e.addRS(t, rs)

	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"
	if err := e.c.UpdateUserLabels(sb, map[string]string{"team": "b"}, nil); err == nil {
		t.Fatal("want error")
	}
	got := e.getRS(t, "sb-1")
	if ExtractUserLabels(got.Labels)["team"] != "a" {
		t.Errorf("labels changed despite error: %v", got.Labels)
	}
	if _, ok := got.Annotations[AnnotationLastAppliedConfiguration]; ok {
		t.Errorf("last-applied written despite error: %v", got.Annotations)
	}
}

// Reads see the latest configuration: GetSandbox returns the new labels from sandbox-data.
func TestGetSandboxReadsLatestLabels(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"team": "a"})
	e.addRS(t, rs)

	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"
	if err := e.c.UpdateUserLabels(sb, map[string]string{"team": "b"}, nil); err != nil {
		t.Fatal(err)
	}
	read, err := e.c.GetSandbox(e.getRS(t, "sb-1"))
	if err != nil {
		t.Fatal(err)
	}
	if read.Metadata[MetadataLabels] != "team=b" || ExtractUserLabels(read.ReplicaSet.Labels)["team"] != "b" {
		t.Errorf("sandbox-data metadata %v / ReplicaSet labels %v, want team=b in both", read.Metadata, read.ReplicaSet.Labels)
	}
}

// Resending the same labels writes nothing: no ReplicaSet update, no rotation.
func TestUpdateUserLabelsNoChangeSkipsWrite(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"team": "a"})
	rs.Spec.Template.Labels[UserLabelPrefix+"team"] = "a"
	e.addRS(t, rs)
	before := e.getRS(t, "sb-1")

	sb := &Sandbox{ReplicaSet: rs.DeepCopy(), Metadata: map[string]string{MetadataLabels: "team=a"}}
	sb.Name = "sb-1"
	if err := e.c.UpdateUserLabels(sb, map[string]string{"team": "a"}, nil); err != nil {
		t.Fatal(err)
	}
	after := e.getRS(t, "sb-1")
	if after.ResourceVersion != before.ResourceVersion {
		t.Errorf("ReplicaSet written for a no-op update: %s -> %s", before.ResourceVersion, after.ResourceVersion)
	}
	if _, ok := after.Annotations[AnnotationLastAppliedConfiguration]; ok {
		t.Errorf("no-op update rotated: %v", after.Annotations)
	}
	// sb still gets the latest ReplicaSet, so the snapshot that follows does not conflict.
	if sb.ReplicaSet == nil || sb.ReplicaSet.ResourceVersion != after.ResourceVersion {
		t.Errorf("sb.ReplicaSet not refreshed")
	}
}

// sandbox-data unchanged but the ReplicaSet drifted from it: the ReplicaSet is still repaired.
func TestUpdateUserLabelsNoChangeRepairsDrift(t *testing.T) {
	e := setupFakeEnv(t)
	rs := newLabeledRS("sb-1", "user-a", map[string]string{"team": "a"})
	rs.Labels[UserLabelPrefix+"drift"] = "x"
	e.addRS(t, rs)

	sb := &Sandbox{ReplicaSet: rs.DeepCopy()}
	sb.Name = "sb-1"
	if err := e.c.UpdateUserLabels(sb, map[string]string{"team": "a"}, nil); err != nil {
		t.Fatal(err)
	}
	got := e.getRS(t, "sb-1")
	if u := ExtractUserLabels(got.Labels); !reflect.DeepEqual(u, map[string]string{"team": "a"}) {
		t.Errorf("drift not repaired: %v", u)
	}
	if _, ok := got.Annotations[AnnotationLastAppliedConfiguration]; ok {
		t.Errorf("sandbox-data unchanged, but rotated: %v", got.Annotations)
	}
}

func TestApplyConfigUpdateReportsChange(t *testing.T) {
	rs := rsWithConfig(`{"name":"sb-1","metadata":{"labels":"team=a"}}`, "")
	if changed, err := applyConfigUpdate(rs, setLabelsConfig(map[string]string{"team": "a"})); err != nil || changed {
		t.Errorf("same labels: changed=%v err=%v, want false", changed, err)
	}
	if changed, err := applyConfigUpdate(rs, setLabelsConfig(map[string]string{"team": "b"})); err != nil || !changed {
		t.Errorf("new labels: changed=%v err=%v, want true", changed, err)
	}
}
