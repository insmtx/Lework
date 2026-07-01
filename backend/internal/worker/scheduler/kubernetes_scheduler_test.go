package scheduler

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestWorkerContainerImageFindsNamedWorkerContainer(t *testing.T) {
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "sidecar", Image: "sidecar:v1"},
						{Name: defaultWorkerContainerName, Image: "registry.yygu.cn/insmtx/leros-worker:v2"},
					},
				},
			},
		},
	}

	got := workerContainerImage(deployment)
	if got != "registry.yygu.cn/insmtx/leros-worker:v2" {
		t.Fatalf("worker image = %q", got)
	}
}

func TestWorkerContainerImageFallsBackForSingleContainer(t *testing.T) {
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "legacy-worker", Image: "registry.yygu.cn/insmtx/leros-worker:v1"},
					},
				},
			},
		},
	}

	got := workerContainerImage(deployment)
	if got != "registry.yygu.cn/insmtx/leros-worker:v1" {
		t.Fatalf("worker image = %q", got)
	}
}
