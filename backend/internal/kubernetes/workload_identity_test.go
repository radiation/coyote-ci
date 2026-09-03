package kubernetes

import (
	"context"
	"errors"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestWorkloadIdentityVerifierBindsReviewedTokenToPodAndExecution(t *testing.T) {
	client := &fakeWorkloadIdentityClient{review: validTokenReview(), pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "helper-pod", Namespace: "ci", UID: types.UID("pod-1"), Labels: map[string]string{"coyote-ci.io/execution-job-id": "job-1"}}}}
	verifier, err := NewWorkloadIdentityVerifierWithClient(client, "coyote-workspace-helper")
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	identity, verifyErr := verifier.VerifyWorkspaceHelper(context.Background(), "projected-token", "job-1", "pod-1", domain.WorkspaceHelperRolePrepare)
	if verifyErr != nil || identity.ExecutionJobID != "job-1" || identity.PodUID != "pod-1" {
		t.Fatalf("identity=%#v err=%v", identity, verifyErr)
	}
	if len(client.review.Spec.Audiences) != 1 || client.review.Spec.Audiences[0] != workspaceHelperPrepareAudience {
		t.Fatalf("audiences=%v", client.review.Spec.Audiences)
	}
}

func TestWorkloadIdentityVerifierRejectsUntrustedBindings(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*fakeWorkloadIdentityClient)
	}{
		{name: "wrong audience", mutate: func(c *fakeWorkloadIdentityClient) {
			c.review.Status.Audiences = []string{workspaceHelperPublishAudience}
		}},
		{name: "wrong pod uid", mutate: func(c *fakeWorkloadIdentityClient) { c.pod.UID = types.UID("other") }},
		{name: "wrong execution label", mutate: func(c *fakeWorkloadIdentityClient) { c.pod.Labels["coyote-ci.io/execution-job-id"] = "job-2" }},
		{name: "token review error", mutate: func(c *fakeWorkloadIdentityClient) { c.reviewErr = errors.New("denied") }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeWorkloadIdentityClient{review: validTokenReview(), pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "helper-pod", Namespace: "ci", UID: types.UID("pod-1"), Labels: map[string]string{"coyote-ci.io/execution-job-id": "job-1"}}}}
			testCase.mutate(client)
			verifier, err := NewWorkloadIdentityVerifierWithClient(client, "coyote-workspace-helper")
			if err != nil {
				t.Fatalf("new verifier: %v", err)
			}
			if _, verifyErr := verifier.VerifyWorkspaceHelper(context.Background(), "projected-token", "job-1", "pod-1", domain.WorkspaceHelperRolePrepare); !errors.Is(verifyErr, ErrWorkloadIdentityUnauthorized) {
				t.Fatalf("verify error=%v", verifyErr)
			}
		})
	}
}

func validTokenReview() *authenticationv1.TokenReview {
	return &authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{Authenticated: true, Audiences: []string{workspaceHelperPrepareAudience}, User: authenticationv1.UserInfo{Username: "system:serviceaccount:ci:coyote-workspace-helper", Extra: map[string]authenticationv1.ExtraValue{"authentication.kubernetes.io/pod-namespace": {"ci"}, "authentication.kubernetes.io/pod-name": {"helper-pod"}, "authentication.kubernetes.io/pod-uid": {"pod-1"}}}}}
}

type fakeWorkloadIdentityClient struct {
	review    *authenticationv1.TokenReview
	pod       *corev1.Pod
	reviewErr error
	podErr    error
}

func (c *fakeWorkloadIdentityClient) CreateTokenReview(_ context.Context, review *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error) {
	c.review.Spec = review.Spec
	return c.review, c.reviewErr
}
func (c *fakeWorkloadIdentityClient) GetPod(context.Context, string, string) (*corev1.Pod, error) {
	return c.pod, c.podErr
}
