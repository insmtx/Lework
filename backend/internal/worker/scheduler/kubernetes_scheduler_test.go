package scheduler

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/worker"
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

func TestKubernetesWorkspacePathsUseSingleWorkspaceDirectory(t *testing.T) {
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{}}

	if got, want := scheduler.workspacePath(1, 2), "/data/workspace"; got != want {
		t.Fatalf("workspace host path = %q, want %q", got, want)
	}
	if got, want := scheduler.workspaceMountPath(1, 2), "/workspace"; got != want {
		t.Fatalf("workspace mount path = %q, want %q", got, want)
	}

	scheduler.config.WorkspaceHostPathRoot = "/mnt/leros"
	scheduler.config.WorkspaceMountRoot = "/worker-space"
	if got, want := scheduler.workspacePath(3, 4), "/mnt/leros"; got != want {
		t.Fatalf("custom workspace host path = %q, want %q", got, want)
	}
	if got, want := scheduler.workspaceMountPath(3, 4), "/worker-space"; got != want {
		t.Fatalf("custom workspace mount path = %q, want %q", got, want)
	}
}

func TestKubernetesWorkspaceSpecDriftDetectsLegacyWorkspacePath(t *testing.T) {
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{}}
	spec := &worker.WorkerSpec{
		OrgID:    1,
		WorkerID: 1,
	}
	deployment := scheduler.buildDeployment(spec)
	if scheduler.workspaceSpecDrifted(deployment, spec) {
		t.Fatal("fresh deployment should match desired workspace spec")
	}

	deployment.Spec.Template.Spec.Volumes[1].HostPath.Path = "/data/leros-workspaces"
	workerContainer := &deployment.Spec.Template.Spec.Containers[0]
	workerContainer.VolumeMounts[1].MountPath = "/leros-workspaces"
	for i := range workerContainer.Env {
		if workerContainer.Env[i].Name == "LEROS_WORKSPACE_ROOT" {
			workerContainer.Env[i].Value = "/leros-workspaces"
		}
	}
	for i := 0; i+1 < len(workerContainer.Args); i++ {
		if workerContainer.Args[i] == "--workspace-root" {
			workerContainer.Args[i+1] = "/leros-workspaces"
		}
	}
	deployment.Spec.Template.Spec.InitContainers[0].Args = []string{"chmod -R 0777 /leros-workspaces"}
	deployment.Spec.Template.Spec.InitContainers[0].VolumeMounts[0].MountPath = "/leros-workspaces"

	if !scheduler.workspaceSpecDrifted(deployment, spec) {
		t.Fatal("legacy workspace paths should require reconcile")
	}
}
