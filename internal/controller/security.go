/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	corev1 "k8s.io/api/core/v1"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

// AnyUIDServiceAccountName is the ServiceAccount a game pod runs under when
// its GameTemplate opts into the anyuid escape hatch. Cluster-admin must
// bind this SA to OpenShift's anyuid SCC out of band; the operator never
// grants SCCs itself. Same name is used on plain K8s for consistency.
const AnyUIDServiceAccountName = "gameserver-anyuid"

// DefaultServiceAccountName is used for restricted-profile game pods. It is
// created by the operator in each target namespace on demand and gets no
// special SCC binding — the default SCC on both OCP and plain K8s admits it.
const DefaultServiceAccountName = "gameserver-restricted"

// PodSecurityContext returns the pod-level securityContext for the given
// profile. On OCP the runAsUser/fsGroup are intentionally left unset so
// the namespace UID range annotations populate them; on plain K8s the pod
// admits with defaults.
func PodSecurityContext(profile gameserverv1alpha1.SecurityProfile) *corev1.PodSecurityContext {
	seccomp := &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	if profile == gameserverv1alpha1.SecurityProfileAnyUID {
		// anyuid: don't force non-root because the point is to allow
		// image-declared UIDs (often root or a fixed non-root like 1000).
		return &corev1.PodSecurityContext{
			SeccompProfile: seccomp,
		}
	}
	nonRoot := true
	return &corev1.PodSecurityContext{
		RunAsNonRoot:   &nonRoot,
		SeccompProfile: seccomp,
	}
}

// ContainerSecurityContext returns the container-level securityContext.
// Identical for both profiles: no privileged, no privilege escalation,
// drop all capabilities. Restricted-v2 accepts this; anyuid does too.
func ContainerSecurityContext(profile gameserverv1alpha1.SecurityProfile) *corev1.SecurityContext {
	allowPriv := false
	priv := false
	return &corev1.SecurityContext{
		Privileged:               &priv,
		AllowPrivilegeEscalation: &allowPriv,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// ServiceAccountForProfile returns the name of the ServiceAccount a game pod
// should run under, given its profile.
func ServiceAccountForProfile(profile gameserverv1alpha1.SecurityProfile) string {
	if profile == gameserverv1alpha1.SecurityProfileAnyUID {
		return AnyUIDServiceAccountName
	}
	return DefaultServiceAccountName
}

// ManagerContainerSecurityContext is the securityContext applied to the
// operator manager itself. Stricter than game pods: read-only root FS is on.
func ManagerContainerSecurityContext() *corev1.SecurityContext {
	allowPriv := false
	priv := false
	readOnly := true
	return &corev1.SecurityContext{
		Privileged:               &priv,
		AllowPrivilegeEscalation: &allowPriv,
		ReadOnlyRootFilesystem:   &readOnly,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}
