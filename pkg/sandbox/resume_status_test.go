package sandbox

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	reconcilertesting "knative.dev/pkg/reconciler/testing"

	_ "knative.dev/pkg/client/injection/kube/client/fake"
)

func TestPatchResumeStatusOnlyTouchesItsAnnotation(t *testing.T) {
	prev := config.Cfg
	config.Cfg = &config.Config{SandboxNamespace: "default"}
	t.Cleanup(func() { config.Cfg = prev })

	ctx, _ := reconcilertesting.SetupFakeContext(t)
	c := NewController(ctx, nil, nil, nil)
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: "sb-1", Namespace: "default",
		Annotations: map[string]string{AnnotationProcessSnapshot: "snap", AnnotationResumeReason: "Request"},
	}}
	if _, err := c.kclient.AppsV1().ReplicaSets("default").Create(context.TODO(), rs, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	sb := &Sandbox{SandboxBase: SandboxBase{Name: "sb-1"}}
	c.patchResumeStatus(sb, ResumeStatus{
		PodReady: true, SnapshotRestored: false, Total: 2, Started: 1, Error: "start process 2/2: boom",
	})
	if sb.ReplicaSet == nil || sb.ReplicaSet.Annotations[AnnotationResumeStatus] == "" {
		t.Errorf("caller's sandbox not updated with the patched ReplicaSet")
	}

	got, err := c.kclient.AppsV1().ReplicaSets("default").Get(context.TODO(), "sb-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Annotations[AnnotationProcessSnapshot] != "snap" || got.Annotations[AnnotationResumeReason] != "Request" {
		t.Errorf("other annotations changed: %v", got.Annotations)
	}
	var st ResumeStatus
	if err := json.Unmarshal([]byte(got.Annotations[AnnotationResumeStatus]), &st); err != nil {
		t.Fatalf("resume status is not JSON: %v", err)
	}
	if !st.PodReady || st.SnapshotRestored || st.Total != 2 || st.Started != 1 || st.Error == "" || st.UpdatedAt.IsZero() {
		t.Errorf("unexpected resume status: %+v", st)
	}
}

func TestRefreshRuntimeMetadata(t *testing.T) {
	sb := &Sandbox{
		SandboxBase: SandboxBase{Name: "sb-1"},
		ReplicaSet: &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			AnnotationProcessSnapshot: "snap",
			AnnotationResumeStatus:    `{"podReady":true,"snapshotRestored":true}`,
			AnnotationResumedAt:       "2026-10-22T08:00:00Z",
			AnnotationResumeReason:    "SDKGetSandbox",
		}}},
	}
	sb.Metadata = map[string]string{"idleTimeout": "600"}
	sb.EnvVars = map[string]string{"FOO": "bar"}

	sb.RefreshRuntimeMetadata()

	t.Log("Metadata after RefreshRuntimeMetadata:", sb)

	want := map[string]string{
		"idleTimeout":        "600",
		MetadataSnapshot:     "snap",
		MetadataResumeStatus: `{"podReady":true,"snapshotRestored":true}`,
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
