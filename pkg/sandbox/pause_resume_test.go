package sandbox

import (
	"context"
	"testing"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	reconcilertesting "knative.dev/pkg/reconciler/testing"

	_ "knative.dev/pkg/client/injection/kube/client/fake"
)

// setPaused works on a stale copy: it patches only the pause fields, in both directions,
// and refreshes the caller's sandbox.
func TestSetPausedBothDirections(t *testing.T) {
	prev := config.Cfg
	config.Cfg = &config.Config{SandboxNamespace: "default"}
	t.Cleanup(func() { config.Cfg = prev })

	ctx, _ := reconcilertesting.SetupFakeContext(t)
	c := NewController(ctx, nil, nil, record.NewFakeRecorder(10))
	replicas := int32(1)
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sb-1", Namespace: "default",
			Annotations: map[string]string{AnnotationProcessSnapshot: "snap"},
		},
		Spec: appsv1.ReplicaSetSpec{Replicas: &replicas},
	}
	if _, err := c.kclient.AppsV1().ReplicaSets("default").Create(context.TODO(), rs, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	get := func() *appsv1.ReplicaSet {
		got, err := c.kclient.AppsV1().ReplicaSets("default").Get(context.TODO(), "sb-1", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// pause: no ReplicaSet in the caller's copy, the patch must not depend on it
	sb := &Sandbox{SandboxBase: SandboxBase{Name: "sb-1"}}
	if err := c.setPaused(sb, true, "resume_failed", ""); err != nil {
		t.Fatal(err)
	}
	if sb.ReplicaSet == nil || sb.Status != Paused {
		t.Errorf("caller's sandbox not updated: status %q, replicaset %v", sb.Status, sb.ReplicaSet)
	}
	got := get()
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 0 {
		t.Errorf("paused replicas = %v, want 0", got.Spec.Replicas)
	}
	if got.Annotations[AnnotationPaused] != "true" || got.Annotations[AnnotationPauseReason] != "resume_failed" || got.Annotations[AnnotationPausedAt] == "" {
		t.Errorf("pause annotations not set: %v", got.Annotations)
	}
	if got.Annotations[AnnotationProcessSnapshot] != "snap" {
		t.Errorf("other annotations changed: %v", got.Annotations)
	}

	// resume: pause marks cleared, resume marks set, resume status starts as resuming
	if err := c.setPaused(sb, false, "Request", got.ResourceVersion); err != nil {
		t.Fatal(err)
	}
	got = get()
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 1 {
		t.Errorf("resumed replicas = %v, want 1", got.Spec.Replicas)
	}
	for _, k := range []string{AnnotationPaused, AnnotationPausedAt, AnnotationPauseReason} {
		if _, ok := got.Annotations[k]; ok {
			t.Errorf("annotation %s not removed: %v", k, got.Annotations)
		}
	}
	if got.Annotations[AnnotationResumeReason] != "Request" || got.Annotations[AnnotationResumedAt] == "" {
		t.Errorf("resume annotations not set: %v", got.Annotations)
	}
	if got.Annotations[AnnotationProcessSnapshot] != "snap" {
		t.Errorf("other annotations changed: %v", got.Annotations)
	}
	if _, running := latestResume(got); !running {
		t.Errorf("resume status not resuming: %q", got.Annotations[AnnotationResumeStatus])
	}
	if sb.Status == Paused {
		t.Errorf("caller's sandbox still paused after resume")
	}
}

func TestRefreshRuntimeMetadata(t *testing.T) {
	sb := &Sandbox{
		SandboxBase: SandboxBase{Name: "sb-1"},
		ReplicaSet: &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			AnnotationProcessSnapshot: "snap",
			AnnotationResumeStatus:    `{"state":"succeeded","podReady":true}`,
			AnnotationResumedAt:       "2026-10-22T08:00:00Z",
			AnnotationResumeReason:    "SDKGetSandbox",
		}}},
	}
	sb.Metadata = map[string]string{"idleTimeout": "600"}
	sb.EnvVars = map[string]string{"FOO": "bar"}

	sb.RefreshRuntimeMetadata()

	want := map[string]string{
		"idleTimeout":        "600",
		MetadataSnapshot:     "snap",
		MetadataResumeStatus: `{"state":"succeeded","podReady":true}`,
		MetadataResumedAt:    "2026-10-22T08:00:00Z",
		MetadataResumeReason: "SDKGetSandbox",
	}
	for k, v := range want {
		if sb.Metadata[k] != v {
			t.Errorf("metadata[%s] = %q, want %q", k, sb.Metadata[k], v)
		}
	}

	// keys without an annotation keep whatever the user set under the same name
	user := &Sandbox{Metadata: map[string]string{MetadataResumedAt: "user-value"}}
	user.ReplicaSet = &appsv1.ReplicaSet{}
	user.RefreshRuntimeMetadata()
	if user.Metadata[MetadataResumedAt] != "user-value" {
		t.Errorf("user metadata overwritten or dropped: %v", user.Metadata)
	}
}

// writeResumeStatus round-trips through latestResume; only a "resuming" state counts as running.
func TestResumeStatusRoundTrip(t *testing.T) {
	prev := config.Cfg
	config.Cfg = &config.Config{SandboxNamespace: "default"}
	t.Cleanup(func() { config.Cfg = prev })

	ctx, _ := reconcilertesting.SetupFakeContext(t)
	c := NewController(ctx, nil, nil, record.NewFakeRecorder(10))
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: "sb-1", Namespace: "default",
		Annotations: map[string]string{AnnotationResumeReason: "Request"},
	}}
	if _, err := c.kclient.AppsV1().ReplicaSets("default").Create(context.TODO(), rs, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	sb := &Sandbox{SandboxBase: SandboxBase{Name: "sb-1"}}
	want := ResumeStatus{State: ResumeStateFailed, PodReady: true, Total: 2, Started: 1, Error: "boom"}
	if err := c.writeResumeStatus(sb, want); err != nil {
		t.Fatal(err)
	}
	got, running := latestResume(sb.ReplicaSet)
	if got.State != want.State || !got.PodReady || got.Total != 2 || got.Started != 1 || got.Error != "boom" || got.UpdatedAt.IsZero() {
		t.Errorf("resume status = %+v", got)
	}
	if running {
		t.Errorf("failed resume counted as running")
	}
	if sb.ReplicaSet.Annotations[AnnotationResumeReason] != "Request" {
		t.Errorf("other annotations changed: %v", sb.ReplicaSet.Annotations)
	}
}
