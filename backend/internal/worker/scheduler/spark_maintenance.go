package scheduler

import (
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/insmtx/Leros/backend/internal/worker"
)

const maintenanceVolume = "spark-maintenance"

func (s *KubernetesScheduler) validateMaintenanceBinding() error {
	b := s.config.SparkMaintenance
	if b == nil {
		return nil
	}
	if b.OrgID == 0 || len(b.WorkerIDs) == 0 || len(b.WorkerIDs) > 32 || b.ReadGroup <= 0 || b.ReadGroup > 2147483647 ||
		b.NodeName == "" || len(validation.IsDNS1123Subdomain(b.NodeName)) != 0 || b.ConfigMap == "" ||
		len(validation.IsDNS1123Subdomain(b.ConfigMap)) != 0 || b.ConfigMap == s.configMap() {
		return fmt.Errorf("invalid Spark worker maintenance binding")
	}
	seen := map[uint]bool{}
	for _, id := range b.WorkerIDs {
		if id == 0 || seen[id] {
			return fmt.Errorf("invalid Spark worker IDs")
		}
		seen[id] = true
	}
	if node := s.config.NodeSelector["kubernetes.io/hostname"]; node != "" && node != b.NodeName {
		return fmt.Errorf("Spark maintenance node conflicts with scheduler node selector")
	}
	return nil
}
func (s *KubernetesScheduler) maintenanceBound(spec *worker.WorkerSpec) bool {
	b := s.config.SparkMaintenance
	if b == nil || b.OrgID != spec.OrgID {
		return false
	}
	for _, id := range b.WorkerIDs {
		if id == spec.WorkerID {
			return true
		}
	}
	return false
}
func (s *KubernetesScheduler) applyMaintenanceBinding(p *corev1.PodSpec, spec *worker.WorkerSpec) {
	if !s.maintenanceBound(spec) {
		return
	}
	b := s.config.SparkMaintenance
	selector := make(map[string]string, len(p.NodeSelector)+1)
	for k, v := range p.NodeSelector {
		selector[k] = v
	}
	selector["kubernetes.io/hostname"] = b.NodeName
	p.NodeSelector = selector
	p.SecurityContext = &corev1.PodSecurityContext{SupplementalGroups: []int64{b.ReadGroup}}
	for i := range p.Volumes {
		if p.Volumes[i].Name == "config" {
			p.Volumes[i].ConfigMap.LocalObjectReference.Name = b.ConfigMap
		}
	}
	p.Volumes = append(p.Volumes, corev1.Volume{Name: maintenanceVolume, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{
		Path: "/var/lib/leros-spark", Type: hostPathTypePtr(corev1.HostPathDirectory)}}})
	p.Containers[0].VolumeMounts = append(p.Containers[0].VolumeMounts, corev1.VolumeMount{Name: maintenanceVolume, MountPath: "/run/leros-spark-state", ReadOnly: true})
}
func (s *KubernetesScheduler) maintenanceSpecDrifted(d *appsv1.Deployment, spec *worker.WorkerSpec) bool {
	if !s.maintenanceBound(spec) {
		return false
	}
	want := s.buildDeployment(spec).Spec.Template.Spec
	got := d.Spec.Template.Spec
	if !reflect.DeepEqual(got.NodeSelector, want.NodeSelector) || !reflect.DeepEqual(got.SecurityContext, want.SecurityContext) {
		return true
	}
	for _, name := range []string{"config", maintenanceVolume} {
		var actual, desired *corev1.Volume
		for i := range got.Volumes {
			if got.Volumes[i].Name == name {
				actual = &got.Volumes[i]
			}
		}
		for i := range want.Volumes {
			if want.Volumes[i].Name == name {
				desired = &want.Volumes[i]
			}
		}
		if !maintenanceVolumesEqual(actual, desired) {
			return true
		}
	}
	container := containerByName(got.Containers, d.Name)
	if container == nil {
		return true
	}
	for _, desired := range want.Containers[0].VolumeMounts {
		if desired.Name != "config" && desired.Name != maintenanceVolume {
			continue
		}
		found := false
		for _, actual := range container.VolumeMounts {
			if actual.Name == desired.Name {
				found = reflect.DeepEqual(actual, desired)
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// 比较 API 默认化后的语义，避免 DefaultMode 的 nil/0644 差异触发重启。
func maintenanceVolumesEqual(actual, desired *corev1.Volume) bool {
	if actual == nil || desired == nil {
		return actual == desired
	}
	a, b := actual.DeepCopy(), desired.DeepCopy()
	for _, v := range []*corev1.Volume{a, b} {
		if v.ConfigMap != nil && v.ConfigMap.DefaultMode == nil {
			mode := int32(corev1.ConfigMapVolumeSourceDefaultMode)
			v.ConfigMap.DefaultMode = &mode
		}
	}
	return reflect.DeepEqual(a, b)
}
