package kubernetes

import (
	"context"
	"io"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewClient(kubeconfig string) (Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &clientset{client: client}, nil
}

type clientset struct{ client kubernetes.Interface }

func (c *clientset) GetJob(ctx context.Context, namespace, name string) (*batchv1.Job, error) {
	return c.client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
}
func (c *clientset) CreateJob(ctx context.Context, namespace string, job *batchv1.Job) (*batchv1.Job, error) {
	return c.client.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
}
func (c *clientset) DeleteJob(ctx context.Context, namespace, name string) error {
	return c.client.BatchV1().Jobs(namespace).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: backgroundDeletion()})
}
func (c *clientset) ListJobs(ctx context.Context, namespace, selector string) ([]batchv1.Job, error) {
	jobs, err := c.client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	return jobs.Items, err
}
func (c *clientset) ListPods(ctx context.Context, namespace, selector string) ([]corev1.Pod, error) {
	pods, err := c.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	return pods.Items, err
}
func (c *clientset) GetPodLogs(ctx context.Context, namespace, name string) (io.ReadCloser, error) {
	return c.client.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{}).Stream(ctx)
}
func backgroundDeletion() *metav1.DeletionPropagation {
	value := metav1.DeletePropagationBackground
	return &value
}
