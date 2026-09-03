package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

const workspaceHelperPrepareAudience = "coyote-ci-workspace-helper-prepare"
const workspaceHelperPublishAudience = "coyote-ci-workspace-helper-publish"

var ErrWorkloadIdentityUnauthorized = errors.New("kubernetes workload identity is unauthorized")

type workloadIdentityClient interface {
	CreateTokenReview(context.Context, *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error)
	GetPod(context.Context, string, string) (*corev1.Pod, error)
}

type WorkloadIdentityVerifier struct {
	client                 workloadIdentityClient
	expectedServiceAccount string
}

func NewWorkloadIdentityVerifier(kubeconfig string, expectedServiceAccount string) (*WorkloadIdentityVerifier, error) {
	config, err := kubernetesConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return NewWorkloadIdentityVerifierWithClient(&workloadIdentityClientset{client: client}, expectedServiceAccount)
}

func NewWorkloadIdentityVerifierWithClient(client workloadIdentityClient, expectedServiceAccount string) (*WorkloadIdentityVerifier, error) {
	if client == nil || strings.TrimSpace(expectedServiceAccount) == "" {
		return nil, errors.New("workload identity verifier requires client and service account")
	}
	return &WorkloadIdentityVerifier{client: client, expectedServiceAccount: strings.TrimSpace(expectedServiceAccount)}, nil
}

func (v *WorkloadIdentityVerifier) VerifyWorkspaceHelper(ctx context.Context, token string, executionJobID string, podUID string, role domain.WorkspaceHelperRole) (service.VerifiedWorkloadIdentity, error) {
	audience, err := workspaceHelperAudience(role)
	if err != nil || strings.TrimSpace(token) == "" || strings.TrimSpace(executionJobID) == "" || strings.TrimSpace(podUID) == "" {
		return service.VerifiedWorkloadIdentity{}, ErrWorkloadIdentityUnauthorized
	}
	review, err := v.client.CreateTokenReview(ctx, &authenticationv1.TokenReview{Spec: authenticationv1.TokenReviewSpec{Token: token, Audiences: []string{audience}}})
	if err != nil || review == nil || !review.Status.Authenticated || !containsString(review.Status.Audiences, audience) {
		return service.VerifiedWorkloadIdentity{}, ErrWorkloadIdentityUnauthorized
	}
	namespace := tokenExtra(review.Status.User.Extra, "authentication.kubernetes.io/pod-namespace")
	podName := tokenExtra(review.Status.User.Extra, "authentication.kubernetes.io/pod-name")
	tokenPodUID := tokenExtra(review.Status.User.Extra, "authentication.kubernetes.io/pod-uid")
	if namespace == "" || podName == "" || tokenPodUID != strings.TrimSpace(podUID) || review.Status.User.Username != "system:serviceaccount:"+namespace+":"+v.expectedServiceAccount {
		return service.VerifiedWorkloadIdentity{}, ErrWorkloadIdentityUnauthorized
	}
	pod, err := v.client.GetPod(ctx, namespace, podName)
	if err != nil || pod == nil || string(pod.UID) != tokenPodUID || pod.Labels["coyote-ci.io/execution-job-id"] != strings.TrimSpace(executionJobID) {
		return service.VerifiedWorkloadIdentity{}, ErrWorkloadIdentityUnauthorized
	}
	return service.VerifiedWorkloadIdentity{ExecutionJobID: strings.TrimSpace(executionJobID), PodUID: tokenPodUID}, nil
}

func workspaceHelperAudience(role domain.WorkspaceHelperRole) (string, error) {
	switch role {
	case domain.WorkspaceHelperRolePrepare:
		return workspaceHelperPrepareAudience, nil
	case domain.WorkspaceHelperRolePublish:
		return workspaceHelperPublishAudience, nil
	default:
		return "", fmt.Errorf("%w: unsupported helper role", ErrWorkloadIdentityUnauthorized)
	}
}

func tokenExtra(extra map[string]authenticationv1.ExtraValue, key string) string {
	values := extra[key]
	if len(values) != 1 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type workloadIdentityClientset struct{ client kubernetes.Interface }

func (c *workloadIdentityClientset) CreateTokenReview(ctx context.Context, review *authenticationv1.TokenReview) (*authenticationv1.TokenReview, error) {
	return c.client.AuthenticationV1().TokenReviews().Create(ctx, review, metav1.CreateOptions{})
}

func (c *workloadIdentityClientset) GetPod(ctx context.Context, namespace string, name string) (*corev1.Pod, error) {
	return c.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
}

func kubernetesConfig(kubeconfig string) (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
