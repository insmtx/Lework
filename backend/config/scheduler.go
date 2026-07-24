package config

// ResourceRequirements 是 worker 容器资源限制的配置表示，键为资源名（cpu/memory），
// 值为 Kubernetes quantity 字符串（如 "500m"、"4Gi"）。由 scheduler 转换为
// corev1.ResourceRequirements 后注入 worker Pod。
type ResourceRequirements struct {
	Limits   map[string]string `yaml:"limits,omitempty" json:"limits,omitempty"`
	Requests map[string]string `yaml:"requests,omitempty" json:"requests,omitempty"`
}

type SchedulerConfig struct {
	Mode                  string               `yaml:"mode,omitempty" json:"mode,omitempty"` // 调度模式，支持 "process","docker-cli","docker-api","k8s"
	WorkerBinary          string               `yaml:"worker_binary,omitempty" json:"worker_binary,omitempty"`
	WorkingDir            string               `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	Env                   map[string]string    `yaml:"env,omitempty" json:"env,omitempty"`
	ServerAddr            string               `yaml:"server_addr,omitempty" json:"server_addr,omitempty"`
	WorkerConfig          string               `yaml:"worker_config,omitempty" json:"worker_config,omitempty"`
	KubernetesConnection  string               `yaml:"kubernetes_connection,omitempty" json:"kubernetes_connection,omitempty"`
	KubeconfigPath        string               `yaml:"kubeconfig_path,omitempty" json:"kubeconfig_path,omitempty"`
	KubeconfigContext     string               `yaml:"kubeconfig_context,omitempty" json:"kubeconfig_context,omitempty"`
	Namespace             string               `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	WorkerImage           string               `yaml:"worker_image,omitempty" json:"worker_image,omitempty"`
	WorkspaceInitImage    string               `yaml:"workspace_init_image,omitempty" json:"workspace_init_image,omitempty"`
	ConfigMap             string               `yaml:"config_map,omitempty" json:"config_map,omitempty"`
	Secret                string               `yaml:"secret,omitempty" json:"secret,omitempty"`
	ImagePullSecret       string               `yaml:"image_pull_secret,omitempty" json:"image_pull_secret,omitempty"`
	WorkspaceHostPathRoot string               `yaml:"workspace_host_path_root,omitempty" json:"workspace_host_path_root,omitempty"`
	WorkspaceMountRoot    string               `yaml:"workspace_mount_root,omitempty" json:"workspace_mount_root,omitempty"`
	StorageHostPath       string               `yaml:"storage_host_path,omitempty" json:"storage_host_path,omitempty"`
	StorageMountPath      string               `yaml:"storage_mount_path,omitempty" json:"storage_mount_path,omitempty"`
	NodeSelector          map[string]string    `yaml:"node_selector,omitempty" json:"node_selector,omitempty"`
	WorkerResources       ResourceRequirements `yaml:"worker_resources,omitempty" json:"worker_resources,omitempty"`
}
