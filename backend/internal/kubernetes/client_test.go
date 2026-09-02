package kubernetes

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestClientsetDelegatesKubernetesResources(t *testing.T) {
	ctx := context.Background()
	fake := kubernetesfake.NewClientset()
	client := &clientset{client: fake}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "ci", Labels: map[string]string{"managed": "yes"}}}
	if _, err := client.CreateJob(ctx, "ci", job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if got, err := client.GetJob(ctx, "ci", "job-1"); err != nil || got.Name != "job-1" {
		t.Fatalf("get job=%v err=%v", got, err)
	}
	if jobs, err := client.ListJobs(ctx, "ci", "managed=yes"); err != nil || len(jobs) != 1 {
		t.Fatalf("list jobs=%d err=%v", len(jobs), err)
	}
	if _, err := fake.CoreV1().Pods("ci").Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Labels: map[string]string{"job-name": "job-1"}}}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	if pods, err := client.ListPods(ctx, "ci", "job-name=job-1"); err != nil || len(pods) != 1 {
		t.Fatalf("list pods=%d err=%v", len(pods), err)
	}
	if err := client.DeleteJob(ctx, "ci", "job-1"); err != nil {
		t.Fatalf("delete job: %v", err)
	}
}

func TestBackgroundDeletion(t *testing.T) {
	if got := backgroundDeletion(); got == nil || *got != metav1.DeletePropagationBackground {
		t.Fatalf("propagation=%v", got)
	}
}
