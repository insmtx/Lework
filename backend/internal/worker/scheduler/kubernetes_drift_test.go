package scheduler

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	typedappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/worker"
)

// stubDeployments 只实现调度器用到的 Get/Create/Update/Delete，其余方法由嵌入的
// 接口兜底；这样测试不需要 client-go/testing 的额外依赖。
type stubDeployments struct {
	typedappsv1.DeploymentInterface
	deployment *appsv1.Deployment
	getErr     error
	updateErrs []error
	updates    []*appsv1.Deployment
	creates    int
	deletes    int
}

func (s *stubDeployments) Get(_ context.Context, name string, _ metav1.GetOptions) (*appsv1.Deployment, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.deployment == nil || s.deployment.Name != name {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "deployments"}, name)
	}
	return s.deployment.DeepCopy(), nil
}

func (s *stubDeployments) Create(_ context.Context, deployment *appsv1.Deployment, _ metav1.CreateOptions) (*appsv1.Deployment, error) {
	s.creates++
	s.deployment = deployment.DeepCopy()
	return deployment.DeepCopy(), nil
}

func (s *stubDeployments) Update(_ context.Context, deployment *appsv1.Deployment, _ metav1.UpdateOptions) (*appsv1.Deployment, error) {
	if len(s.updateErrs) > 0 {
		err := s.updateErrs[0]
		s.updateErrs = s.updateErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	s.updates = append(s.updates, deployment.DeepCopy())
	s.deployment = deployment.DeepCopy()
	return deployment.DeepCopy(), nil
}

func (s *stubDeployments) Delete(_ context.Context, name string, _ metav1.DeleteOptions) error {
	if s.deployment == nil || s.deployment.Name != name {
		return apierrors.NewNotFound(schema.GroupResource{Resource: "deployments"}, name)
	}
	s.deletes++
	s.deployment = nil
	return nil
}

func testWorkerSpec() *worker.WorkerSpec {
	return &worker.WorkerSpec{
		ID:       "leros-worker-o1-w1",
		OrgID:    1,
		WorkerID: 1,
		Image:    "img:v1",
	}
}

func newTestScheduler(t *testing.T, cfg *config.SchedulerConfig, stub *stubDeployments) *KubernetesScheduler {
	t.Helper()
	if cfg == nil {
		cfg = &config.SchedulerConfig{WorkerImage: "img:v1"}
	}
	if stub == nil {
		stub = &stubDeployments{}
	}
	return &KubernetesScheduler{config: cfg, deployments: stub}
}

func containsField(fields []string, want string) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
}

// TestDriftFieldsEmptyForBuiltDeployment 是收敛性的基线：刚 Start 过的 Deployment 不应再判漂移。
func TestDriftFieldsEmptyForBuiltDeployment(t *testing.T) {
	scheduler := newTestScheduler(t, nil, nil)
	spec := testWorkerSpec()

	desired := scheduler.buildDeployment(spec)
	scheduler.deployments = &stubDeployments{deployment: desired}

	fields, err := scheduler.DriftFields(context.Background(), spec)
	if err != nil {
		t.Fatalf("DriftFields() error = %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("DriftFields() = %v, want no drift", fields)
	}
}

// TestDriftFieldsIgnoresAdmissionInjectedResources 覆盖线上循环的最可能真因：
// LimitRange / mutating webhook 注入 resources 后，判据不能永远为真。
func TestDriftFieldsIgnoresAdmissionInjectedResources(t *testing.T) {
	scheduler := newTestScheduler(t, nil, nil)
	spec := testWorkerSpec()

	desired := scheduler.buildDeployment(spec)
	desired.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
	scheduler.deployments = &stubDeployments{deployment: desired}

	fields, err := scheduler.DriftFields(context.Background(), spec)
	if err != nil {
		t.Fatalf("DriftFields() error = %v", err)
	}
	if containsField(fields, "resources") {
		t.Fatalf("DriftFields() = %v, injected resources must not count as drift", fields)
	}
}

// TestDriftFieldsIgnoresExternalEnv 外部注入的 env（代理、监控等）不归 Leros 管。
func TestDriftFieldsIgnoresExternalEnv(t *testing.T) {
	scheduler := newTestScheduler(t, nil, nil)
	spec := testWorkerSpec()

	desired := scheduler.buildDeployment(spec)
	desired.Spec.Template.Spec.Containers[0].Env = append(
		desired.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "HTTP_PROXY", Value: "http://proxy.internal:3128"},
	)
	scheduler.deployments = &stubDeployments{deployment: desired}

	fields, err := scheduler.DriftFields(context.Background(), spec)
	if err != nil {
		t.Fatalf("DriftFields() error = %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("DriftFields() = %v, want external env ignored", fields)
	}
}

// TestDriftFieldsReportsImageDrift 真漂移仍要能被发现，并且日志能指出字段。
func TestDriftFieldsReportsImageDrift(t *testing.T) {
	scheduler := newTestScheduler(t, nil, nil)
	spec := testWorkerSpec()

	desired := scheduler.buildDeployment(spec)
	desired.Spec.Template.Spec.Containers[0].Image = "img:old"
	scheduler.deployments = &stubDeployments{deployment: desired}

	fields, err := scheduler.DriftFields(context.Background(), spec)
	if err != nil {
		t.Fatalf("DriftFields() error = %v", err)
	}
	if !containsField(fields, "image") {
		t.Fatalf("DriftFields() = %v, want image drift", fields)
	}
}

// TestDriftFieldsReportsMissingDeployment 首次创建路径。
func TestDriftFieldsReportsMissingDeployment(t *testing.T) {
	scheduler := newTestScheduler(t, nil, nil)

	fields, err := scheduler.DriftFields(context.Background(), testWorkerSpec())
	if err != nil {
		t.Fatalf("DriftFields() error = %v", err)
	}
	if !containsField(fields, "deployment") {
		t.Fatalf("DriftFields() = %v, want deployment drift", fields)
	}
}

// TestDriftFieldsComparesConfiguredResources 配置显式声明 resources 时仍然要管。
func TestDriftFieldsComparesConfiguredResources(t *testing.T) {
	cfg := &config.SchedulerConfig{
		WorkerImage: "img:v1",
		WorkerResources: config.ResourceRequirements{
			Limits: map[string]string{"cpu": "500m"},
		},
	}
	scheduler := newTestScheduler(t, cfg, nil)
	spec := testWorkerSpec()

	desired := scheduler.buildDeployment(spec)
	scheduler.deployments = &stubDeployments{deployment: desired}

	fields, err := scheduler.DriftFields(context.Background(), spec)
	if err != nil {
		t.Fatalf("DriftFields() error = %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("DriftFields() = %v, want configured resources to match", fields)
	}

	desired.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("2")
	scheduler.deployments = &stubDeployments{deployment: desired}

	fields, err = scheduler.DriftFields(context.Background(), spec)
	if err != nil {
		t.Fatalf("DriftFields() error = %v", err)
	}
	if !containsField(fields, "resources") {
		t.Fatalf("DriftFields() = %v, want resources drift", fields)
	}
}

// TestStartRetriesUpdateOnConflict 覆盖 409：重读 ResourceVersion 后重试一次即成功。
func TestStartRetriesUpdateOnConflict(t *testing.T) {
	scheduler := newTestScheduler(t, nil, nil)
	spec := testWorkerSpec()
	desired := scheduler.buildDeployment(spec)

	conflict := apierrors.NewConflict(
		schema.GroupResource{Resource: "deployments"}, desired.Name,
		errors.New("the object has been modified; please apply your changes to the latest version and try again"),
	)
	stub := &stubDeployments{deployment: desired, updateErrs: []error{conflict, nil}}
	scheduler.deployments = stub

	if _, err := scheduler.Start(context.Background(), spec); err != nil {
		t.Fatalf("Start() error = %v, want retry to succeed", err)
	}
	if len(stub.updates) != 1 {
		t.Fatalf("successful updates = %d, want 1", len(stub.updates))
	}
}

// TestStartCreatesDeploymentWhenMissing 首次创建路径。
func TestStartCreatesDeploymentWhenMissing(t *testing.T) {
	scheduler := newTestScheduler(t, nil, nil)
	stub := &stubDeployments{}
	scheduler.deployments = stub

	if _, err := scheduler.Start(context.Background(), testWorkerSpec()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if stub.creates != 1 {
		t.Fatalf("creates = %d, want 1", stub.creates)
	}
	if len(stub.updates) != 0 {
		t.Fatalf("updates = %d, want 0", len(stub.updates))
	}
}

// TestStartReturnsNonConflictErrorImmediately 非 409 错误不应重试。
func TestStartReturnsNonConflictErrorImmediately(t *testing.T) {
	scheduler := newTestScheduler(t, nil, nil)
	spec := testWorkerSpec()
	desired := scheduler.buildDeployment(spec)
	stub := &stubDeployments{deployment: desired, updateErrs: []error{errors.New("boom")}}
	scheduler.deployments = stub

	if _, err := scheduler.Start(context.Background(), spec); err == nil {
		t.Fatal("Start() error = nil, want update failure")
	}
	if len(stub.updates) != 0 {
		t.Fatalf("successful updates = %d, want 0", len(stub.updates))
	}
}

// TestStartPreservesExternalFieldsOnUpdate 真实漂移修复不应顺手抹掉运维/准入注入的字段。
func TestStartPreservesExternalFieldsOnUpdate(t *testing.T) {
	scheduler := newTestScheduler(t, nil, nil)
	spec := testWorkerSpec()

	current := scheduler.buildDeployment(spec)
	current.Annotations = map[string]string{"ops.yygu.cn/owner": "sre"}
	current.Spec.Template.Spec.Containers[0].Env = append(
		current.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "HTTP_PROXY", Value: "http://proxy.internal:3128"},
	)
	stub := &stubDeployments{deployment: current}
	scheduler.deployments = stub

	if _, err := scheduler.Start(context.Background(), spec); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(stub.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(stub.updates))
	}
	updated := stub.updates[0]
	if got := updated.Annotations["ops.yygu.cn/owner"]; got != "sre" {
		t.Fatalf("annotation = %q, want preserved", got)
	}
	if got := envValue(updated.Spec.Template.Spec.Containers[0].Env, "HTTP_PROXY"); got != "http://proxy.internal:3128" {
		t.Fatalf("external env = %q, want preserved", got)
	}

	ownedCount := 0
	for _, env := range updated.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "LEROS_SERVER_ADDR" {
			ownedCount++
		}
	}
	if ownedCount != 1 {
		t.Fatalf("owned env duplicated %d times, want 1", ownedCount)
	}
}
