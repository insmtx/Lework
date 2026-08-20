package scheduler

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/worker"
)

func TestWorkerContainerImageFindsNamedWorkerContainer(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "leros-worker-o12-w1"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "sidecar", Image: "sidecar:v1"},
						{Name: "leros-worker-o12-w1", Image: "registry.yygu.cn/insmtx/leros-worker:v2"},
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

func TestKubernetesDeploymentUsesDeploymentNameForWorkerContainer(t *testing.T) {
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{}}
	deployment := scheduler.buildDeployment(&worker.WorkerSpec{OrgID: 12, WorkerID: 1})

	if got, want := deployment.Name, "leros-worker-o12-w1"; got != want {
		t.Fatalf("deployment name = %q, want %q", got, want)
	}
	if got, want := deployment.Spec.Template.Spec.Containers[0].Name, deployment.Name; got != want {
		t.Fatalf("worker container name = %q, want deployment name %q", got, want)
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

func TestKubernetesWorkspaceHostPathPerWorkerMountPathShared(t *testing.T) {
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{}}

	if got, want := scheduler.workspacePath(1, 2), "/data/workspace/leros-worker-o1-w2"; got != want {
		t.Fatalf("workspace host path = %q, want %q", got, want)
	}
	if got, want := scheduler.workspaceMountPath(1, 2), "/workspace"; got != want {
		t.Fatalf("workspace mount path = %q, want %q", got, want)
	}

	scheduler.config.WorkspaceHostPathRoot = "/mnt/leros"
	scheduler.config.WorkspaceMountRoot = "/worker-space"
	if got, want := scheduler.workspacePath(3, 4), "/mnt/leros/leros-worker-o3-w4"; got != want {
		t.Fatalf("custom workspace host path = %q, want %q", got, want)
	}
	if got, want := scheduler.workspaceMountPath(3, 4), "/worker-space"; got != want {
		t.Fatalf("custom workspace mount path = %q, want %q", got, want)
	}
}

func TestKubernetesStoragePathSharedWithoutWorkerDirectory(t *testing.T) {
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{}}

	if got, want := scheduler.storageHostPath(), "/data/leros-storage"; got != want {
		t.Fatalf("storage host path = %q, want shared %q", got, want)
	}

	scheduler.config.StorageHostPath = "/data/custom-storage"
	if got, want := scheduler.storageHostPath(), "/data/custom-storage"; got != want {
		t.Fatalf("custom storage host path = %q, want %q", got, want)
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

func TestBuildDeploymentDefaultImagePullPolicy(t *testing.T) {
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{}}
	deployment := scheduler.buildDeployment(&worker.WorkerSpec{OrgID: 1, WorkerID: 1})
	if got := deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy; got != corev1.PullIfNotPresent {
		t.Fatalf("default imagePullPolicy = %q, want PullIfNotPresent, was PullAlways before", got)
	}
}

func TestBuildDeploymentCustomImagePullPolicy(t *testing.T) {
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{WorkerImagePullPolicy: "Always"}}
	deployment := scheduler.buildDeployment(&worker.WorkerSpec{OrgID: 1, WorkerID: 1})
	if got := deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy; got != corev1.PullAlways {
		t.Fatalf("custom imagePullPolicy = %q, want PullAlways", got)
	}
}

func TestBuildDeploymentAppliesWorkerResources(t *testing.T) {
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{
		WorkerResources: config.ResourceRequirements{
			Limits:   map[string]string{"cpu": "2", "memory": "4Gi"},
			Requests: map[string]string{"cpu": "500m", "memory": "1Gi"},
		},
	}}
	deployment := scheduler.buildDeployment(&worker.WorkerSpec{OrgID: 1, WorkerID: 1})
	res := deployment.Spec.Template.Spec.Containers[0].Resources

	if got := res.Limits.Cpu().String(); got != "2" {
		t.Fatalf("limits.cpu = %q, want 2", got)
	}
	if got := res.Limits.Memory().String(); got != "4Gi" {
		t.Fatalf("limits.memory = %q, want 4Gi", got)
	}
	if got := res.Requests.Cpu().String(); got != "500m" {
		t.Fatalf("requests.cpu = %q, want 500m", got)
	}
	if got := res.Requests.Memory().String(); got != "1Gi" {
		t.Fatalf("requests.memory = %q, want 1Gi", got)
	}
}

func TestBuildDeploymentNoResourcesByDefault(t *testing.T) {
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{}}
	deployment := scheduler.buildDeployment(&worker.WorkerSpec{OrgID: 1, WorkerID: 1})
	res := deployment.Spec.Template.Spec.Containers[0].Resources
	if len(res.Limits) != 0 || len(res.Requests) != 0 {
		t.Fatalf("expected empty resources, got limits=%v requests=%v", res.Limits, res.Requests)
	}
}

func TestResourcesDriftedDetectsMissingResources(t *testing.T) {
	spec := &worker.WorkerSpec{OrgID: 5, WorkerID: 3}

	// 期望有资源限制，但现有 deployment 的容器没有设置 → 漂移
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{
		WorkerResources: config.ResourceRequirements{
			Limits: map[string]string{"cpu": "1"},
		},
	}}
	deployment := scheduler.buildDeployment(spec)
	// 清除容器资源模拟旧 deployment
	deployment.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
	if !scheduler.resourcesDrifted(deployment, spec) {
		t.Fatal("missing resources should be detected as drift")
	}
}

func TestResourcesDriftedNoDriftWhenMatch(t *testing.T) {
	spec := &worker.WorkerSpec{OrgID: 5, WorkerID: 3}
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{
		WorkerResources: config.ResourceRequirements{
			Limits:   map[string]string{"cpu": "1", "memory": "2Gi"},
			Requests: map[string]string{"cpu": "100m"},
		},
	}}
	deployment := scheduler.buildDeployment(spec)
	if scheduler.resourcesDrifted(deployment, spec) {
		t.Fatal("matching resources should not be detected as drift")
	}
}

func TestResourcesDriftedDetectsQuantityChange(t *testing.T) {
	spec := &worker.WorkerSpec{OrgID: 5, WorkerID: 3}
	scheduler := &KubernetesScheduler{config: &config.SchedulerConfig{
		WorkerResources: config.ResourceRequirements{
			Limits: map[string]string{"cpu": "2"},
		},
	}}
	deployment := scheduler.buildDeployment(spec)
	// 修改现有容器资源为不同的值
	deployment.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
	}
	if !scheduler.resourcesDrifted(deployment, spec) {
		t.Fatal("different cpu limit should be detected as drift")
	}
}
