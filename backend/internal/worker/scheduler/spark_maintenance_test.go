package scheduler

import (
	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/worker"
	corev1 "k8s.io/api/core/v1"
	"testing"
)

func TestSparkMaintenanceBindingSurvivesRebuildAndDetectsDrift(t *testing.T) {
	s := &KubernetesScheduler{config: &config.SchedulerConfig{SparkMaintenance: &config.SparkWorkerMaintenanceBinding{
		OrgID: 7, WorkerIDs: []uint{9}, NodeName: "master", ConfigMap: "spark-worker-7-9", ReadGroup: 1000}}}
	spec := &worker.WorkerSpec{OrgID: 7, WorkerID: 9}
	if err := s.validateMaintenanceBinding(); err != nil {
		t.Fatal(err)
	}
	d := s.buildDeployment(spec)
	p := &d.Spec.Template.Spec
	if p.NodeSelector["kubernetes.io/hostname"] != "master" || p.SecurityContext.SupplementalGroups[0] != 1000 {
		t.Fatal("host binding missing")
	}
	var mounted bool
	for _, m := range p.Containers[0].VolumeMounts {
		if m.Name == "spark-maintenance" {
			mounted = m.ReadOnly && m.SubPath == "" && m.MountPath == "/run/leros-spark-state"
		}
	}
	if !mounted {
		t.Fatal("missing read-only directory mount")
	}
	if p.Volumes[0].ConfigMap.Name != "spark-worker-7-9" {
		t.Fatal("uses shared config")
	}
	if s.maintenanceSpecDrifted(d, spec) {
		t.Fatal("fresh deployment drifted")
	}
	for _, mutate := range []func(*corev1.PodSpec){
		func(p *corev1.PodSpec) { p.Volumes = p.Volumes[:len(p.Volumes)-1] },
		func(p *corev1.PodSpec) { p.NodeSelector["kubernetes.io/hostname"] = "other" },
		func(p *corev1.PodSpec) { p.SecurityContext.SupplementalGroups = nil },
		func(p *corev1.PodSpec) { p.Volumes[0].ConfigMap.Name = "shared" },
		func(p *corev1.PodSpec) {
			for i := range p.Containers[0].VolumeMounts {
				if p.Containers[0].VolumeMounts[i].Name == "spark-maintenance" {
					p.Containers[0].VolumeMounts[i].ReadOnly = false
				}
			}
		},
	} {
		changed := d.DeepCopy()
		mutate(&changed.Spec.Template.Spec)
		if !s.maintenanceSpecDrifted(changed, spec) {
			t.Fatal("unsafe drift ignored")
		}
	}
	other := s.buildDeployment(&worker.WorkerSpec{OrgID: 8, WorkerID: 9})
	for _, v := range other.Spec.Template.Spec.Volumes {
		if v.Name == "spark-maintenance" {
			t.Fatal("foreign worker received host state")
		}
	}
	if other.Spec.Template.Spec.Volumes[0].ConfigMap.Name == "spark-worker-7-9" {
		t.Fatal("foreign worker received private config")
	}
}
func TestSparkMaintenanceBindingRejectsSharedConfigAndConflictingNode(t *testing.T) {
	for _, mutate := range []func(*config.SchedulerConfig){
		func(c *config.SchedulerConfig) { c.SparkMaintenance.ConfigMap = "leros-worker-config" },
		func(c *config.SchedulerConfig) { c.SparkMaintenance.WorkerIDs = []uint{9, 9} },
		func(c *config.SchedulerConfig) { c.NodeSelector = map[string]string{"kubernetes.io/hostname": "other"} },
		func(c *config.SchedulerConfig) { c.SparkMaintenance.ReadGroup = 0 },
	} {
		c := &config.SchedulerConfig{SparkMaintenance: &config.SparkWorkerMaintenanceBinding{OrgID: 7, WorkerIDs: []uint{9}, NodeName: "master", ConfigMap: "spark-worker", ReadGroup: 1000}}
		mutate(c)
		s := &KubernetesScheduler{config: c}
		if s.validateMaintenanceBinding() == nil {
			t.Fatal("invalid binding accepted")
		}
	}
}

func TestSparkMaintenanceIgnoresAPIServerConfigMapDefaultMode(t *testing.T) {
	s := &KubernetesScheduler{config: &config.SchedulerConfig{SparkMaintenance: &config.SparkWorkerMaintenanceBinding{OrgID: 7, WorkerIDs: []uint{9}, NodeName: "master", ConfigMap: "spark-worker", ReadGroup: 1000}}}
	spec := &worker.WorkerSpec{OrgID: 7, WorkerID: 9}
	d := s.buildDeployment(spec)
	mode := int32(0644)
	for i := range d.Spec.Template.Spec.Volumes {
		v := &d.Spec.Template.Spec.Volumes[i]
		if v.ConfigMap != nil {
			v.ConfigMap.DefaultMode = &mode
		}
	}
	if s.maintenanceSpecDrifted(d, spec) {
		t.Fatal("API-defaulted file mode causes an unnecessary Worker restart")
	}
}
