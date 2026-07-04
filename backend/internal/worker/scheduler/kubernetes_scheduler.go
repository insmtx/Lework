package scheduler

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/worker"
)

const (
	defaultKubernetesNamespace         = "default"
	defaultKubernetesConnection        = "auto"
	defaultWorkerContainerName         = "leros-worker"
	defaultWorkspaceInitContainerName  = "init-workspace"
	defaultWorkerListenAddr            = ":8081"
	defaultWorkspaceContainerMountRoot = "/workspace"
	defaultWorkerConfigMountPath       = "/app/config"
	defaultWorkerConfigFile            = "/app/config/config.yaml"
	defaultWorkspaceHostPathRoot       = "/data/workspace"
	defaultStorageHostPath             = "/data/leros-storage"
	defaultStorageMountPath            = "/leros-storage"
	defaultWorkerImage                 = "leros-worker:local"
	defaultWorkspaceInitImage          = "busybox:1.36.1"
)

type KubernetesScheduler struct {
	config *config.SchedulerConfig
	client kubernetes.Interface
}

var _ worker.WorkerScheduler = (*KubernetesScheduler)(nil)
var _ worker.WorkerSpecReconciler = (*KubernetesScheduler)(nil)

func NewKubernetesScheduler(cfg *config.SchedulerConfig) (worker.WorkerScheduler, error) {
	if cfg == nil {
		cfg = &config.SchedulerConfig{}
	}
	restCfg, err := kubernetesRESTConfig(cfg)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return &KubernetesScheduler{config: cfg, client: client}, nil
}

func kubernetesRESTConfig(cfg *config.SchedulerConfig) (*rest.Config, error) {
	mode := strings.TrimSpace(cfg.KubernetesConnection)
	if mode == "" {
		mode = defaultKubernetesConnection
	}
	switch mode {
	case "in_cluster":
		return rest.InClusterConfig()
	case "kubeconfig":
		return kubeconfigRESTConfig(cfg)
	case "auto":
		if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
			if restCfg, inErr := rest.InClusterConfig(); inErr == nil {
				return restCfg, nil
			}
		}
		return kubeconfigRESTConfig(cfg)
	default:
		return nil, fmt.Errorf("unsupported kubernetes_connection: %s", mode)
	}
}

func kubeconfigRESTConfig(cfg *config.SchedulerConfig) (*rest.Config, error) {
	overrides := &clientcmd.ConfigOverrides{}
	if contextName := strings.TrimSpace(cfg.KubeconfigContext); contextName != "" {
		overrides.CurrentContext = contextName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: strings.TrimSpace(cfg.KubeconfigPath)},
		overrides,
	).ClientConfig()
}

func (s *KubernetesScheduler) Start(ctx context.Context, spec *worker.WorkerSpec) (*worker.WorkerInstance, error) {
	if spec == nil {
		return nil, fmt.Errorf("worker spec is required")
	}
	if spec.OrgID == 0 {
		return nil, fmt.Errorf("org_id is required")
	}
	if spec.WorkerID == 0 {
		return nil, fmt.Errorf("worker_id is required")
	}
	deployment := s.buildDeployment(spec)
	existing, err := s.client.AppsV1().Deployments(s.namespace()).Get(ctx, deployment.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := s.client.AppsV1().Deployments(s.namespace()).Create(ctx, deployment, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("create worker deployment: %w", err)
		}
		return s.instanceFromDeployment(deployment, "created"), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get worker deployment: %w", err)
	}
	deployment.ResourceVersion = existing.ResourceVersion
	if _, err := s.client.AppsV1().Deployments(s.namespace()).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("update worker deployment: %w", err)
	}
	return s.instanceFromDeployment(deployment, "updated"), nil
}

func (s *KubernetesScheduler) Stop(ctx context.Context, workerID string) error {
	if strings.TrimSpace(workerID) == "" {
		return fmt.Errorf("worker_id is required")
	}
	err := s.client.AppsV1().Deployments(s.namespace()).Delete(ctx, workerID, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (s *KubernetesScheduler) Health(ctx context.Context, workerID string) error {
	deployment, err := s.client.AppsV1().Deployments(s.namespace()).Get(ctx, workerID, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("%w: %s", worker.ErrWorkerNotFound, workerID)
	}
	if err != nil {
		return err
	}
	if deployment.Status.ReadyReplicas < 1 {
		return fmt.Errorf("deployment %s is not ready", workerID)
	}
	return nil
}

func (s *KubernetesScheduler) List(ctx context.Context) ([]*worker.WorkerInstance, error) {
	list, err := s.client.AppsV1().Deployments(s.namespace()).List(ctx, metav1.ListOptions{
		LabelSelector: "app=leros,component=worker",
	})
	if err != nil {
		return nil, err
	}
	result := make([]*worker.WorkerInstance, 0, len(list.Items))
	for i := range list.Items {
		deployment := list.Items[i]
		status := "not-ready"
		if deployment.Status.ReadyReplicas > 0 {
			status = "ready"
		}
		result = append(result, s.instanceFromDeployment(&deployment, status))
	}
	return result, nil
}

func (s *KubernetesScheduler) NeedsReconcile(ctx context.Context, spec *worker.WorkerSpec) (bool, error) {
	if spec == nil {
		return false, fmt.Errorf("worker spec is required")
	}
	if spec.OrgID == 0 {
		return false, fmt.Errorf("org_id is required")
	}
	if spec.WorkerID == 0 {
		return false, fmt.Errorf("worker_id is required")
	}
	deployment, err := s.client.AppsV1().Deployments(s.namespace()).Get(ctx, deploymentName(spec.OrgID, spec.WorkerID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get worker deployment: %w", err)
	}
	currentImage := workerContainerImage(deployment)
	if currentImage == "" {
		return true, nil
	}
	if currentImage != s.workerImage(spec) {
		return true, nil
	}
	return s.workspaceSpecDrifted(deployment, spec), nil
}

func (s *KubernetesScheduler) buildDeployment(spec *worker.WorkerSpec) *appsv1.Deployment {
	name := deploymentName(spec.OrgID, spec.WorkerID)
	workspaceMountPath := s.workspaceMountPath(spec.OrgID, spec.WorkerID)
	labels := map[string]string{
		"app":                "leros",
		"component":          "worker",
		"leros.io/org-id":    strconv.FormatUint(uint64(spec.OrgID), 10),
		"leros.io/worker-id": strconv.FormatUint(uint64(spec.WorkerID), 10),
	}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	replicas := int32(1)
	env := []corev1.EnvVar{
		{Name: "LEROS_ORG_ID", Value: strconv.FormatUint(uint64(spec.OrgID), 10)},
		{Name: "LEROS_WORKER_ID", Value: strconv.FormatUint(uint64(spec.WorkerID), 10)},
		{Name: "LEROS_SERVER_ADDR", Value: s.serverAddr(spec)},
		{Name: "LEROS_WORKSPACE_ROOT", Value: workspaceMountPath},
	}
	if spec.BootstrapToken != "" {
		env = append(env, corev1.EnvVar{Name: "LEROS_WORKER_BOOTSTRAP_TOKEN", Value: spec.BootstrapToken})
	}
	for key, value := range s.config.Env {
		env = append(env, corev1.EnvVar{Name: key, Value: value})
	}
	secretName := strings.TrimSpace(s.config.Secret)
	if secretName != "" {
		env = append(env, corev1.EnvVar{
			Name: "LLM_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "LLM_API_KEY",
					Optional:             boolPtr(true),
				},
			},
		})
	}
	args := []string{
		"worker",
		"--org-id", strconv.FormatUint(uint64(spec.OrgID), 10),
		"--worker-id", strconv.FormatUint(uint64(spec.WorkerID), 10),
		"--config", defaultWorkerConfigFile,
		"--workspace-root", workspaceMountPath,
		"--listen-addr", defaultWorkerListenAddr,
	}
	if spec.BootstrapToken != "" {
		args = append(args, "--bootstrap-token", spec.BootstrapToken)
	}
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: s.configMap()},
				},
			},
		},
		{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: s.workspacePath(spec.OrgID, spec.WorkerID),
					Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
	}
	if storageHostPath := s.storageHostPath(); storageHostPath != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "leros-storage",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: storageHostPath,
					Type: hostPathTypePtr(corev1.HostPathDirectoryOrCreate),
				},
			},
		})
	}
	volumeMounts := []corev1.VolumeMount{
		{Name: "config", MountPath: defaultWorkerConfigMountPath, ReadOnly: true},
		{Name: "workspace", MountPath: workspaceMountPath},
	}
	if storageMountPath := s.storageMountPath(); storageMountPath != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{Name: "leros-storage", MountPath: storageMountPath})
	}
	podSpec := corev1.PodSpec{
		NodeSelector: s.config.NodeSelector,
		InitContainers: []corev1.Container{
			{
				Name:    defaultWorkspaceInitContainerName,
				Image:   s.workspaceInitImage(),
				Command: []string{"sh", "-c"},
				Args:    []string{"chmod -R 0777 " + workspaceMountPath},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: int64Ptr(0),
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: workspaceMountPath}},
			},
		},
		Containers: []corev1.Container{
			{
				Name:            defaultWorkerContainerName,
				Image:           s.workerImage(spec),
				ImagePullPolicy: corev1.PullAlways,
				Command:         []string{"/leros"},
				Args:            args,
				Env:             env,
				VolumeMounts:    volumeMounts,
			},
		},
		Volumes: volumes,
	}
	if pullSecret := strings.TrimSpace(s.config.ImagePullSecret); pullSecret != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: pullSecret}}
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace(), Labels: labels, Annotations: spec.Annotations},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}
}

func workerContainerImage(deployment *appsv1.Deployment) string {
	if deployment == nil {
		return ""
	}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == defaultWorkerContainerName {
			return strings.TrimSpace(container.Image)
		}
	}
	if len(deployment.Spec.Template.Spec.Containers) == 1 {
		return strings.TrimSpace(deployment.Spec.Template.Spec.Containers[0].Image)
	}
	return ""
}

func (s *KubernetesScheduler) workspaceSpecDrifted(deployment *appsv1.Deployment, spec *worker.WorkerSpec) bool {
	if deployment == nil || spec == nil {
		return true
	}
	desiredHostPath := s.workspacePath(spec.OrgID, spec.WorkerID)
	desiredMountPath := s.workspaceMountPath(spec.OrgID, spec.WorkerID)
	podSpec := deployment.Spec.Template.Spec
	if hostPathForVolume(podSpec.Volumes, "workspace") != desiredHostPath {
		return true
	}
	workerContainer := containerByName(podSpec.Containers, defaultWorkerContainerName)
	if workerContainer == nil {
		return true
	}
	if mountPathForVolume(workerContainer.VolumeMounts, "workspace") != desiredMountPath {
		return true
	}
	if envValue(workerContainer.Env, "LEROS_WORKSPACE_ROOT") != desiredMountPath {
		return true
	}
	if argValue(workerContainer.Args, "--workspace-root") != desiredMountPath {
		return true
	}
	initContainer := containerByName(podSpec.InitContainers, defaultWorkspaceInitContainerName)
	if initContainer == nil {
		return true
	}
	if mountPathForVolume(initContainer.VolumeMounts, "workspace") != desiredMountPath {
		return true
	}
	return len(initContainer.Args) == 0 || initContainer.Args[0] != "chmod -R 0777 "+desiredMountPath
}

func containerByName(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

func hostPathForVolume(volumes []corev1.Volume, name string) string {
	for _, volume := range volumes {
		if volume.Name == name && volume.HostPath != nil {
			return strings.TrimSpace(volume.HostPath.Path)
		}
	}
	return ""
}

func mountPathForVolume(mounts []corev1.VolumeMount, name string) string {
	for _, mount := range mounts {
		if mount.Name == name {
			return strings.TrimSpace(mount.MountPath)
		}
	}
	return ""
}

func envValue(env []corev1.EnvVar, name string) string {
	for _, item := range env {
		if item.Name == name {
			return strings.TrimSpace(item.Value)
		}
	}
	return ""
}

func argValue(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return strings.TrimSpace(args[i+1])
		}
	}
	return ""
}

func (s *KubernetesScheduler) instanceFromDeployment(deployment *appsv1.Deployment, status string) *worker.WorkerInstance {
	return &worker.WorkerInstance{
		ID:        deployment.Name,
		WorkerID:  deployment.Name,
		Status:    status,
		StartedAt: time.Now(),
		Endpoint:  deployment.Name,
	}
}

func (s *KubernetesScheduler) namespace() string {
	if value := strings.TrimSpace(s.config.Namespace); value != "" {
		return value
	}
	return defaultKubernetesNamespace
}

func (s *KubernetesScheduler) configMap() string {
	if value := strings.TrimSpace(s.config.ConfigMap); value != "" {
		return value
	}
	return "leros-worker-config"
}

func (s *KubernetesScheduler) workerImage(spec *worker.WorkerSpec) string {
	if value := strings.TrimSpace(spec.Image); value != "" {
		return value
	}
	if value := strings.TrimSpace(s.config.WorkerImage); value != "" {
		return value
	}
	return defaultWorkerImage
}

func (s *KubernetesScheduler) workspaceInitImage() string {
	if value := strings.TrimSpace(s.config.WorkspaceInitImage); value != "" {
		return value
	}
	return defaultWorkspaceInitImage
}

func (s *KubernetesScheduler) serverAddr(spec *worker.WorkerSpec) string {
	if value := strings.TrimSpace(spec.ServerAddr); value != "" {
		return value
	}
	return strings.TrimSpace(s.config.ServerAddr)
}

func (s *KubernetesScheduler) workspacePath(_, _ uint) string {
	root := strings.TrimSpace(s.config.WorkspaceHostPathRoot)
	if root == "" {
		root = defaultWorkspaceHostPathRoot
	}
	return root
}

func (s *KubernetesScheduler) workspaceMountPath(_, _ uint) string {
	root := strings.TrimSpace(s.config.WorkspaceMountRoot)
	if root == "" {
		root = defaultWorkspaceContainerMountRoot
	}
	return root
}

func (s *KubernetesScheduler) storageHostPath() string {
	if value := strings.TrimSpace(s.config.StorageHostPath); value != "" {
		return value
	}
	return defaultStorageHostPath
}

func (s *KubernetesScheduler) storageMountPath() string {
	if value := strings.TrimSpace(s.config.StorageMountPath); value != "" {
		return value
	}
	return defaultStorageMountPath
}

func deploymentName(orgID, workerID uint) string {
	return fmt.Sprintf("leros-worker-o%d-w%d", orgID, workerID)
}

func boolPtr(value bool) *bool                                       { return &value }
func int64Ptr(value int64) *int64                                    { return &value }
func hostPathTypePtr(value corev1.HostPathType) *corev1.HostPathType { return &value }
