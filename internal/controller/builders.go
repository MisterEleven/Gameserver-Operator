/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

const (
	appName        = "gameserver"
	managedByValue = "gameserver-operator"

	labelName      = "app.kubernetes.io/name"
	labelInstance  = "app.kubernetes.io/instance"
	labelManagedBy = "app.kubernetes.io/managed-by"
	labelComponent = "app.kubernetes.io/component"
	labelTemplate  = "gameserver.feddern.dev/template"

	dataVolumeName = "data"
)

// baseLabels are attached to every child object owned by a GameServer.
func baseLabels(gs *gameserverv1alpha1.GameServer) map[string]string {
	return map[string]string{
		labelName:      appName,
		labelInstance:  gs.Name,
		labelManagedBy: managedByValue,
		labelTemplate:  gs.Spec.TemplateRef.Name,
	}
}

// selectorLabels are the subset stable across the pod's lifecycle; used as
// the Deployment/Service selector.
func selectorLabels(gs *gameserverv1alpha1.GameServer) map[string]string {
	return map[string]string{
		labelName:     appName,
		labelInstance: gs.Name,
	}
}

// PVCName is the deterministic name of the world PVC for this GameServer.
func PVCName(gs *gameserverv1alpha1.GameServer) string {
	return gs.Name + "-data"
}

// DeploymentName mirrors the GameServer name; kubebuilder's short name for
// GameServer is `gsv` so no risk of collision in typical usage.
func DeploymentName(gs *gameserverv1alpha1.GameServer) string {
	return gs.Name
}

// ServiceName is `<gs>-<expose-lowercase>` so multiple exposure types can
// coexist without collision.
func ServiceName(gs *gameserverv1alpha1.GameServer, exposeAs gameserverv1alpha1.ExposureType) string {
	return fmt.Sprintf("%s-%s", gs.Name, strings.ToLower(string(exposeAs)))
}

// snapshotAPIGroup is the API group name used in PVC dataSourceRef when
// restoring from a VolumeSnapshot. Kept as a package-level var so tests
// can compare pointer values without shadowing the corev1 API.
var snapshotAPIGroup = "snapshot.storage.k8s.io"

// BuildPVC returns the PVC for the game's persistent data.
//
// When sourceSnapshot is non-nil, the PVC is created with a
// spec.dataSourceRef pointing at the given VolumeSnapshot in the same
// namespace. Any CSI driver that advertises snapshotting picks this up
// via the standard snapshot.storage.k8s.io/v1 API — nothing here is
// driver-specific.
func BuildPVC(gs *gameserverv1alpha1.GameServer, tpl *gameserverv1alpha1.GameTemplate, sourceSnapshot *string) (*corev1.PersistentVolumeClaim, error) {
	size := gs.Spec.Storage.Size
	if size == "" {
		size = tpl.Spec.Storage.DefaultSize
	}
	if size == "" {
		size = "10Gi"
	}
	qty, err := resource.ParseQuantity(size)
	if err != nil {
		return nil, fmt.Errorf("invalid storage size %q: %w", size, err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PVCName(gs),
			Namespace: gs.Namespace,
			Labels:    baseLabels(gs),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: qty,
				},
			},
		},
	}
	if class := gs.Spec.Storage.ClassName; class != "" {
		pvc.Spec.StorageClassName = &class
	}
	if sourceSnapshot != nil && *sourceSnapshot != "" {
		vsKind := "VolumeSnapshot"
		pvc.Spec.DataSourceRef = &corev1.TypedObjectReference{
			APIGroup: &snapshotAPIGroup,
			Kind:     vsKind,
			Name:     *sourceSnapshot,
		}
	}
	return pvc, nil
}

// resolveEnv computes the final env slice for the container, in order:
// template.Env, then defaults from configKeys, then gs.Config values, then
// gs.EnvOverride (last write wins for a given name).
func resolveEnv(gs *gameserverv1alpha1.GameServer, tpl *gameserverv1alpha1.GameTemplate) []corev1.EnvVar {
	byName := map[string]corev1.EnvVar{}
	order := []string{}
	add := func(e corev1.EnvVar) {
		if _, seen := byName[e.Name]; !seen {
			order = append(order, e.Name)
		}
		byName[e.Name] = e
	}

	for _, e := range tpl.Spec.Env {
		add(e)
	}
	for _, key := range tpl.Spec.ConfigKeys {
		if key.Default == "" {
			continue
		}
		add(corev1.EnvVar{Name: key.EnvVar, Value: key.Default})
	}
	// build a lookup so unknown keys in gs.Config are ignored by builders
	// (they'll be flagged separately by ValidateConfig at reconcile time).
	byConfigName := map[string]gameserverv1alpha1.ConfigKey{}
	for _, key := range tpl.Spec.ConfigKeys {
		byConfigName[key.Name] = key
	}
	// deterministic ordering over map iteration.
	names := make([]string, 0, len(gs.Spec.Config))
	for k := range gs.Spec.Config {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		key, ok := byConfigName[k]
		if !ok {
			continue
		}
		add(corev1.EnvVar{Name: key.EnvVar, Value: gs.Spec.Config[k]})
	}
	for _, e := range gs.Spec.EnvOverride {
		add(e)
	}

	out := make([]corev1.EnvVar, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out
}

// resolveExposure returns the effective ExposeAs for a template port after
// applying any per-instance override.
func resolveExposure(gs *gameserverv1alpha1.GameServer, p gameserverv1alpha1.TemplatePort) (gameserverv1alpha1.ExposureType, int32) {
	exposure := p.ExposeAs
	if exposure == "" {
		exposure = gameserverv1alpha1.ExposureClusterIP
	}
	var nodePort int32
	for _, o := range gs.Spec.ExposeOverride {
		if o.PortName == p.Name {
			exposure = o.ExposeAs
			nodePort = o.NodePort
			break
		}
	}
	return exposure, nodePort
}

// primaryPort returns the port marked primary, or the first TCP port,
// or the first port. Its resolved exposure is returned as well.
func primaryPort(gs *gameserverv1alpha1.GameServer, tpl *gameserverv1alpha1.GameTemplate) (gameserverv1alpha1.TemplatePort, gameserverv1alpha1.ExposureType, int32) {
	if len(tpl.Spec.Ports) == 0 {
		return gameserverv1alpha1.TemplatePort{}, "", 0
	}
	// pass 1: explicitly primary
	for _, p := range tpl.Spec.Ports {
		if p.Primary {
			exp, np := resolveExposure(gs, p)
			return p, exp, np
		}
	}
	// pass 2: first TCP
	for _, p := range tpl.Spec.Ports {
		if p.Protocol == "" || p.Protocol == corev1.ProtocolTCP {
			exp, np := resolveExposure(gs, p)
			return p, exp, np
		}
	}
	// fallback: first
	p := tpl.Spec.Ports[0]
	exp, np := resolveExposure(gs, p)
	return p, exp, np
}

// BuildDeployment materializes the pod that runs the game server.
func BuildDeployment(gs *gameserverv1alpha1.GameServer, tpl *gameserverv1alpha1.GameTemplate) *appsv1.Deployment {
	strategy := appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	if tpl.Spec.UpdateStrategy == gameserverv1alpha1.UpdateRollingUpdate {
		strategy = appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType}
	}

	pullPolicy := tpl.Spec.ImagePullPolicy
	if pullPolicy == "" {
		pullPolicy = corev1.PullIfNotPresent
	}

	dataPath := tpl.Spec.Storage.DataPath
	if dataPath == "" {
		dataPath = "/data"
	}

	containerPorts := make([]corev1.ContainerPort, 0, len(tpl.Spec.Ports))
	for _, p := range tpl.Spec.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = corev1.ProtocolTCP
		}
		containerPorts = append(containerPorts, corev1.ContainerPort{
			Name:          p.Name,
			ContainerPort: p.ContainerPort,
			Protocol:      proto,
		})
	}

	container := corev1.Container{
		Name:            "game",
		Image:           tpl.Spec.Image,
		ImagePullPolicy: pullPolicy,
		Command:         tpl.Spec.Command,
		Args:            tpl.Spec.Args,
		Env:             resolveEnv(gs, tpl),
		Ports:           containerPorts,
		Resources:       gs.Spec.Resources,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      dataVolumeName,
				MountPath: dataPath,
				SubPath:   tpl.Spec.Storage.SubPath,
			},
		},
		SecurityContext: ContainerSecurityContext(tpl.Spec.SecurityProfile),
	}

	// Probes: use template-provided if any; else fall back to TCP on primary port.
	if tpl.Spec.Probes != nil {
		container.LivenessProbe = tpl.Spec.Probes.Liveness
		container.ReadinessProbe = tpl.Spec.Probes.Readiness
	} else if p, _, _ := primaryPort(gs, tpl); p.ContainerPort > 0 {
		tcp := &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(int(p.ContainerPort)),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			FailureThreshold:    3,
		}
		container.ReadinessProbe = tcp
	}

	replicas := int32(1)
	if gs.Spec.Suspend {
		replicas = 0
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(gs),
			Namespace: gs.Namespace,
			Labels:    baseLabels(gs),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: strategy,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels(gs)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: baseLabels(gs),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: ServiceAccountForProfile(tpl.Spec.SecurityProfile),
					SecurityContext:    PodSecurityContext(tpl.Spec.SecurityProfile),
					Containers:         []corev1.Container{container},
					NodeSelector:       gs.Spec.NodeSelector,
					Tolerations:        gs.Spec.Tolerations,
					Volumes: []corev1.Volume{
						{
							Name: dataVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: PVCName(gs),
								},
							},
						},
					},
				},
			},
		},
	}
	return dep
}

// BuildServices groups the template's ports by their effective exposure
// type and returns one Service per group. A given GameServer will have
// between 1 and 3 Services (ClusterIP, NodePort, LoadBalancer).
func BuildServices(gs *gameserverv1alpha1.GameServer, tpl *gameserverv1alpha1.GameTemplate) []*corev1.Service {
	groups := map[gameserverv1alpha1.ExposureType][]corev1.ServicePort{}
	// preserve encounter order for reproducibility
	order := []gameserverv1alpha1.ExposureType{}

	for _, p := range tpl.Spec.Ports {
		exposure, nodePort := resolveExposure(gs, p)
		proto := p.Protocol
		if proto == "" {
			proto = corev1.ProtocolTCP
		}
		port := corev1.ServicePort{
			Name:       p.Name,
			Port:       p.ContainerPort,
			TargetPort: intstr.FromInt(int(p.ContainerPort)),
			Protocol:   proto,
		}
		if exposure == gameserverv1alpha1.ExposureNodePort && nodePort > 0 {
			port.NodePort = nodePort
		}
		if _, ok := groups[exposure]; !ok {
			order = append(order, exposure)
		}
		groups[exposure] = append(groups[exposure], port)
	}

	services := make([]*corev1.Service, 0, len(order))
	for _, exp := range order {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ServiceName(gs, exp),
				Namespace: gs.Namespace,
				Labels:    baseLabels(gs),
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceType(exp),
				Selector: selectorLabels(gs),
				Ports:    groups[exp],
			},
		}
		services = append(services, svc)
	}
	return services
}

// PrimaryAddress derives the .status.address string from the resolved
// primary port and its assigned Service. Returns empty when unavailable.
func PrimaryAddress(gs *gameserverv1alpha1.GameServer, tpl *gameserverv1alpha1.GameTemplate, services []*corev1.Service) string {
	p, exp, np := primaryPort(gs, tpl)
	if p.ContainerPort == 0 {
		return ""
	}
	svcName := ServiceName(gs, exp)
	var svc *corev1.Service
	for _, s := range services {
		if s.Name == svcName {
			svc = s
			break
		}
	}
	if svc == nil {
		return ""
	}
	switch exp {
	case gameserverv1alpha1.ExposureClusterIP:
		host := svc.Spec.ClusterIP
		if host == "" || host == "None" {
			host = fmt.Sprintf("%s.%s.svc", svcName, gs.Namespace)
		}
		return host + ":" + strconv.Itoa(int(p.ContainerPort))
	case gameserverv1alpha1.ExposureNodePort:
		port := np
		if port == 0 {
			for _, sp := range svc.Spec.Ports {
				if sp.Name == p.Name {
					port = sp.NodePort
					break
				}
			}
		}
		if port == 0 {
			return ""
		}
		return "<node>:" + strconv.Itoa(int(port))
	case gameserverv1alpha1.ExposureLoadBalancer:
		if len(svc.Status.LoadBalancer.Ingress) == 0 {
			return ""
		}
		ing := svc.Status.LoadBalancer.Ingress[0]
		host := ing.Hostname
		if host == "" {
			host = ing.IP
		}
		if host == "" {
			return ""
		}
		return host + ":" + strconv.Itoa(int(p.ContainerPort))
	}
	return ""
}

// ValidateConfig reports the first problem it finds in gs.Spec.Config
// relative to the template's ConfigKeys. Returns nil when all good.
func ValidateConfig(gs *gameserverv1alpha1.GameServer, tpl *gameserverv1alpha1.GameTemplate) error {
	byName := map[string]gameserverv1alpha1.ConfigKey{}
	for _, k := range tpl.Spec.ConfigKeys {
		byName[k.Name] = k
	}
	for k, v := range gs.Spec.Config {
		key, ok := byName[k]
		if !ok {
			return fmt.Errorf("unknown config key %q (template %q allows: %s)", k, tpl.Name, joinKeys(byName))
		}
		switch key.Type {
		case "", "string":
			// no-op
		case "int":
			if _, err := strconv.Atoi(v); err != nil {
				return fmt.Errorf("config key %q expects int, got %q", k, v)
			}
		case "bool":
			if _, err := strconv.ParseBool(v); err != nil {
				return fmt.Errorf("config key %q expects bool, got %q", k, v)
			}
		case "enum":
			valid := false
			for _, e := range key.Enum {
				if e == v {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("config key %q must be one of %v, got %q", k, key.Enum, v)
			}
		}
	}
	for _, key := range tpl.Spec.ConfigKeys {
		if !key.Required {
			continue
		}
		if _, present := gs.Spec.Config[key.Name]; present {
			continue
		}
		if key.Default != "" {
			continue
		}
		return fmt.Errorf("required config key %q missing (template %q)", key.Name, tpl.Name)
	}
	return nil
}

func joinKeys(m map[string]gameserverv1alpha1.ConfigKey) string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
