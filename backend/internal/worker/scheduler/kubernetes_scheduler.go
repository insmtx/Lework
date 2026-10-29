package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	typedappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

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
	// deployments 允许单测注入轻量实现；生产路径由 client 构造。
	deployments typedappsv1.DeploymentInterface
}

// deploymentsAPI 统一取 Deployment 客户端，避免各处重复拼 namespace。
func (s *KubernetesScheduler) deploymentsAPI() typedappsv1.DeploymentInterface {
	if s.deployments != nil {
		return s.deployments
	}
	return s.deploymentsAPI()
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
	if err := validateWorkerSpec(spec); err != nil {
		return nil, err
	}
	deployment := s.buildDeployment(spec)
	api := s.deploymentsAPI()
	_, err := api.Get(ctx, deployment.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := api.Create(ctx, deployment, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("create worker deployment: %w", err)
		}
		return s.instanceFromDeployment(deployment, "created"), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get worker deployment: %w", err)
	}

	// 冲突（409）在多副本 server 或高频 reconcile 下会出现。直接返回会让 reconciler
	// 标 Failed，下一轮再重来；这里按 k8s 惯例重读最新 ResourceVersion 后重试。
	var applied *appsv1.Deployment
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current, getErr := api.Get(ctx, deployment.Name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		next := deployment.DeepCopy()
		s.preserveExternalFields(next, current)
		next.ResourceVersion = current.ResourceVersion
		updated, updateErr := api.Update(ctx, next, metav1.UpdateOptions{})
		if updateErr != nil {
			return updateErr
		}
		applied = updated
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update worker deployment: %w", err)
	}
	if applied == nil {
		applied = deployment
	}
	return s.instanceFromDeployment(applied, "updated"), nil
}

// CurrentBootstrapToken 返回线上 Deployment 正在使用的 bootstrap token。
// 读不到（Deployment 不存在）时返回空串，由 reconciler 决定是否生成新 token。
func (s *KubernetesScheduler) CurrentBootstrapToken(ctx context.Context, deploymentName string) (string, error) {
	if strings.TrimSpace(deploymentName) == "" {
		return "", fmt.Errorf("deployment name is required")
	}
	deployment, err := s.deploymentsAPI().Get(ctx, deploymentName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return bootstrapTokenFromDeployment(deployment), nil
}

// bootstrapTokenFromDeployment 从 env 或 args 里取出 token；env 是主路径，args 兼容旧部署。
func bootstrapTokenFromDeployment(deployment *appsv1.Deployment) string {
	if deployment == nil {
		return ""
	}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if token := envValue(container.Env, "LEROS_WORKER_BOOTSTRAP_TOKEN"); token != "" {
			return token
		}
	}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if token := argValue(container.Args, "--bootstrap-token"); token != "" {
			return token
		}
	}
	return ""
}

func (s *KubernetesScheduler) Stop(ctx context.Context, workerID string) error {
	if strings.TrimSpace(workerID) == "" {
		return fmt.Errorf("worker_id is required")
	}
	err := s.deploymentsAPI().Delete(ctx, workerID, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (s *KubernetesScheduler) Health(ctx context.Context, workerID string) error {
	deployment, err := s.deploymentsAPI().Get(ctx, workerID, metav1.GetOptions{})
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
	list, err := s.deploymentsAPI().List(ctx, metav1.ListOptions{
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

func (s *KubernetesScheduler) Shutdown(ctx context.Context) error {
	return nil
}

// validateWorkerSpec 集中校验调度所需的最小字段，避免 Start/漂移检查各写一份。
func validateWorkerSpec(spec *worker.WorkerSpec) error {
	if spec == nil {
		return fmt.Errorf("worker spec is required")
	}
	if spec.OrgID == 0 {
		return fmt.Errorf("org_id is required")
	}
	if spec.WorkerID == 0 {
		return fmt.Errorf("worker_id is required")
	}
	return nil
}

// DriftFields 返回与期望 spec 不一致的“自有字段”名，用于 reconciler 日志与判定。
//
// 只比较 Leros 自己写入的字段：resources 仅在配置显式声明时比较，准入控制注入的
// resources/env 一律忽略——否则 LimitRange 之类的默认值会让 drift 永远为真，
// 每轮 reconcile 都全量覆盖一次，Pod 被 Recreate 反复替换。
func (s *KubernetesScheduler) DriftFields(ctx context.Context, spec *worker.WorkerSpec) ([]string, error) {
	if err := validateWorkerSpec(spec); err != nil {
		return nil, err
	}
	deployment, err := s.deploymentsAPI().Get(
		ctx, deploymentName(spec.OrgID, spec.WorkerID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return []string{"deployment"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get worker deployment: %w", err)
	}
	return s.driftFields(deployment, spec), nil
}

func (s *KubernetesScheduler) NeedsReconcile(ctx context.Context, spec *worker.WorkerSpec) (bool, error) {
	fields, err := s.DriftFields(ctx, spec)
	if err != nil {
		return false, err
	}
	return len(fields) > 0, nil
}

// driftFields 是 DriftFields 的纯函数部分，便于单测直接喂 Deployment。
func (s *KubernetesScheduler) driftFields(deployment *appsv1.Deployment, spec *worker.WorkerSpec) []string {
	if deployment == nil {
		return []string{"deployment"}
	}
	mountPath := s.workspaceMountPath(spec.OrgID, spec.WorkerID)
	name := deploymentName(spec.OrgID, spec.WorkerID)
	container := containerByName(deployment.Spec.Template.Spec.Containers, name)
	if container == nil {
		container = containerByName(deployment.Spec.Template.Spec.Containers, defaultWorkerContainerName)
	}
	if container == nil {
		return []string{"container"}
	}

	fields := make([]string, 0, 8)
	add := func(field string) { fields = append(fields, field) }

	if container.Name != name {
		add("container.name")
	}
	if strings.TrimSpace(container.Image) != s.workerImage(spec) {
		add("image")
	}
	if container.ImagePullPolicy != s.workerImagePullPolicy() {
		add("imagePullPolicy")
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		add("replicas")
	}

	expectedEnv := map[string]string{
		"LEROS_ORG_ID":         strconv.FormatUint(uint64(spec.OrgID), 10),
		"LEROS_WORKER_ID":      strconv.FormatUint(uint64(spec.WorkerID), 10),
		"LEROS_SERVER_ADDR":    s.serverAddr(spec),
		"LEROS_WORKSPACE_ROOT": mountPath,
	}
	for _, key := range sortedStringKeys(expectedEnv) {
		if envValue(container.Env, key) != expectedEnv[key] {
			add("env." + key)
		}
	}
	// 配置注入的 env 只比 key 是否一致，不把值写进日志（可能含密钥）。
	for _, key := range sortedStringKeys(s.config.Env) {
		if envValue(container.Env, key) != strings.TrimSpace(s.config.Env[key]) {
			add("env." + key)
		}
	}

	expectedArgs := map[string]string{
		"--config":         defaultWorkerConfigFile,
		"--workspace-root": mountPath,
		"--listen-addr":    defaultWorkerListenAddr,
	}
	for _, flag := range sortedStringKeys(expectedArgs) {
		if argValue(container.Args, flag) != expectedArgs[flag] {
			add("arg." + flag)
		}
	}

	if mountPathForVolume(container.VolumeMounts, "workspace") != mountPath {
		add("workspace.mountPath")
	}
	if hostPathForVolume(deployment.Spec.Template.Spec.Volumes, "workspace") != s.workspacePath(spec.OrgID, spec.WorkerID) {
		add("workspace.hostPath")
	}
	if configMapVolumeName(deployment.Spec.Template.Spec.Volumes, "config") != s.configMap() {
		add("configMap")
	}

	if storageHostPath := s.storageHostPath(); storageHostPath != "" {
		if hostPathForVolume(deployment.Spec.Template.Spec.Volumes, "leros-storage") != storageHostPath {
			add("storage.hostPath")
		}
		if mountPathForVolume(container.VolumeMounts, "leros-storage") != s.storageMountPath() {
			add("storage.mountPath")
		}
	}

	initContainer := containerByName(deployment.Spec.Template.Spec.InitContainers, defaultWorkspaceInitContainerName)
	switch {
	case initContainer == nil:
		add("initContainer")
	case mountPathForVolume(initContainer.VolumeMounts, "workspace") != mountPath:
		add("initContainer.workspace")
	case len(initContainer.Args) == 0 || initContainer.Args[0] != "chmod -R 0777 "+mountPath:
		add("initContainer.args")
	}

	// resources 只有配置显式声明时才归 Leros 管；为空时忽略准入控制注入的默认值。
	desiredResources := s.workerResources()
	if !resourceRequirementsEmpty(desiredResources) &&
		!resourceRequirementsEqual(desiredResources, container.Resources) {
		add("resources")
	}

	return fields
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
				Name:            name,
				Image:           s.workerImage(spec),
				ImagePullPolicy: s.workerImagePullPolicy(),
				Command:         []string{"/leros"},
				Args:            args,
				Env:             env,
				VolumeMounts:    volumeMounts,
				Resources:       s.workerResources(),
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
		if container.Name == deployment.Name {
			return strings.TrimSpace(container.Image)
		}
	}
	// Keep recognizing deployments created before worker container names were
	// aligned with their deployment names.
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

// configMapVolumeName 返回 config 卷引用的 ConfigMap 名，用于 drift 判定。
func configMapVolumeName(volumes []corev1.Volume, name string) string {
	for _, volume := range volumes {
		if volume.Name == name && volume.ConfigMap != nil {
			return strings.TrimSpace(volume.ConfigMap.Name)
		}
	}
	return ""
}

// resourceRequirementsEmpty 判断期望资源是否为空；为空表示 Leros 不管理 resources，
// 因而忽略准入控制（如 LimitRange）注入的默认值。
func resourceRequirementsEmpty(r corev1.ResourceRequirements) bool {
	return len(r.Limits) == 0 && len(r.Requests) == 0
}

// sortedStringKeys 让 drift 字段顺序稳定，便于日志比对。
func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

// ownedEnvNames 是 Leros 自己写入、允许被 Update 覆盖的 env 名。
func (s *KubernetesScheduler) ownedEnvNames() map[string]struct{} {
	owned := map[string]struct{}{
		"LEROS_ORG_ID":                 {},
		"LEROS_WORKER_ID":              {},
		"LEROS_SERVER_ADDR":            {},
		"LEROS_WORKSPACE_ROOT":         {},
		"LEROS_WORKER_BOOTSTRAP_TOKEN": {},
		"LLM_API_KEY":                  {},
	}
	for key := range s.config.Env {
		owned[key] = struct{}{}
	}
	return owned
}

// preserveExternalFields 把当前 Deployment 上非 Leros 拥有的字段合并回期望 spec。
// 全量 Update 会抹掉准入控制（LimitRange / webhook）与运维注入的 annotations、
// labels、env；虽然 drift 判据已经忽略它们，但一次真实漂移修复不应顺手清掉。
func (s *KubernetesScheduler) preserveExternalFields(next, current *appsv1.Deployment) {
	if next == nil || current == nil {
		return
	}
	next.Annotations = mergeStringMaps(current.Annotations, next.Annotations)
	next.Labels = mergeStringMaps(current.Labels, next.Labels)
	next.Spec.Template.Annotations = mergeStringMaps(current.Spec.Template.Annotations, next.Spec.Template.Annotations)
	next.Spec.Template.Labels = mergeStringMaps(current.Spec.Template.Labels, next.Spec.Template.Labels)

	if len(next.Spec.Template.Spec.Containers) == 0 || len(current.Spec.Template.Spec.Containers) == 0 {
		return
	}
	owned := s.ownedEnvNames()
	target := &next.Spec.Template.Spec.Containers[0]
	for _, env := range current.Spec.Template.Spec.Containers[0].Env {
		if _, ok := owned[env.Name]; ok {
			continue
		}
		if envValue(target.Env, env.Name) != "" {
			continue
		}
		target.Env = append(target.Env, env)
	}
}

// mergeStringMaps 返回 base 与 override 的并集，override 优先。
func mergeStringMaps(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
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

func (s *KubernetesScheduler) workerImagePullPolicy() corev1.PullPolicy {
	switch strings.ToLower(strings.TrimSpace(s.config.WorkerImagePullPolicy)) {
	case "always", "pullalways":
		return corev1.PullAlways
	case "never", "pullnever":
		return corev1.PullNever
	default:
		return corev1.PullIfNotPresent
	}
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

func (s *KubernetesScheduler) workspacePath(orgID, workerID uint) string {
	root := strings.TrimSpace(s.config.WorkspaceHostPathRoot)
	if root == "" {
		root = defaultWorkspaceHostPathRoot
	}
	return joinHostPath(root, orgID, workerID)
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

// joinHostPath 在宿主根路径下追加 Deployment 名称二级目录，使每个 worker
// 使用独立的 workspace 宿主目录（如 /data/workspace/leros-worker-o1001-w3），
// 目录名与 Deployment 名称一一对应，实现物理隔离。
// workerID 为 0 时返回根路径本身，避免拼出无意义的目录。
func joinHostPath(root string, orgID, workerID uint) string {
	if workerID == 0 {
		return root
	}
	return filepath.Join(root, deploymentName(orgID, workerID))
}

func boolPtr(value bool) *bool                                       { return &value }
func int64Ptr(value int64) *int64                                    { return &value }
func hostPathTypePtr(value corev1.HostPathType) *corev1.HostPathType { return &value }

// workerResources 将配置层 ResourceRequirements 转为 corev1.ResourceRequirements，
// 忽略无法解析的 quantity 字符串。
func (s *KubernetesScheduler) workerResources() corev1.ResourceRequirements {
	return toCoreV1Resources(s.config.WorkerResources)
}

func toCoreV1Resources(r config.ResourceRequirements) corev1.ResourceRequirements {
	req := corev1.ResourceRequirements{}
	for k, v := range r.Limits {
		if q, err := resource.ParseQuantity(strings.TrimSpace(v)); err == nil {
			if req.Limits == nil {
				req.Limits = corev1.ResourceList{}
			}
			req.Limits[corev1.ResourceName(k)] = q
		}
	}
	for k, v := range r.Requests {
		if q, err := resource.ParseQuantity(strings.TrimSpace(v)); err == nil {
			if req.Requests == nil {
				req.Requests = corev1.ResourceList{}
			}
			req.Requests[corev1.ResourceName(k)] = q
		}
	}
	return req
}

func resourceRequirementsEqual(a, b corev1.ResourceRequirements) bool {
	return resourceListEqual(a.Limits, b.Limits) && resourceListEqual(a.Requests, b.Requests)
}

func resourceListEqual(a, b corev1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok || v.Cmp(bv) != 0 {
			return false
		}
	}
	return true
}
