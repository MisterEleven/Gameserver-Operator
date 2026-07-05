/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// GameServerPhase is a coarse-grained lifecycle summary. Detailed truth
// lives in .status.conditions.
// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Degraded;Stopped
type GameServerPhase string

const (
	PhasePending      GameServerPhase = "Pending"
	PhaseProvisioning GameServerPhase = "Provisioning"
	PhaseReady        GameServerPhase = "Ready"
	PhaseDegraded     GameServerPhase = "Degraded"
	PhaseStopped      GameServerPhase = "Stopped"
)

// TemplateRef names the cluster-scoped GameTemplate this server uses.
type TemplateRef struct {
	// name of the GameTemplate.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// StorageOverride lets a GameServer pick a different PVC size or class
// than the template's defaults.
type StorageOverride struct {
	// className overrides the default StorageClass.
	// Leave empty to use the cluster default.
	// +optional
	ClassName string `json:"className,omitempty"`

	// size overrides the template's defaultSize (e.g. "20Gi").
	// +optional
	Size string `json:"size,omitempty"`
}

// ExposureOverride overrides the template's Service exposure for one port.
type ExposureOverride struct {
	// portName targets a template port by its .name.
	// +kubebuilder:validation:MinLength=1
	// +required
	PortName string `json:"portName"`

	// exposeAs replaces the template's ExposeAs for this port.
	// +required
	ExposeAs ExposureType `json:"exposeAs"`

	// nodePort optionally pins a NodePort value; only honored when
	// exposeAs=NodePort and the cluster's port range allows it.
	// +kubebuilder:validation:Minimum=30000
	// +kubebuilder:validation:Maximum=32767
	// +optional
	NodePort int32 `json:"nodePort,omitempty"`
}

// GameServerSpec defines the desired state of one running game server.
type GameServerSpec struct {
	// templateRef selects the GameTemplate this server derives from.
	// +required
	TemplateRef TemplateRef `json:"templateRef"`

	// config carries per-instance values validated against the template's
	// configKeys. Unknown keys are rejected at reconcile time (and at admit
	// time once the webhook lands).
	// +optional
	Config map[string]string `json:"config,omitempty"`

	// storage overrides template defaults for the world PVC.
	// +optional
	Storage StorageOverride `json:"storage,omitempty"`

	// resources sets container CPU/memory requests and limits. Missing
	// values fall through to the container runtime default.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// nodeSelector pins the game pod to matching nodes.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// tolerations allow scheduling on tainted nodes.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// exposeOverride replaces the template's Service exposure per port.
	// +optional
	ExposeOverride []ExposureOverride `json:"exposeOverride,omitempty"`

	// suspend, if true, scales the game pod to 0 replicas without touching
	// the PVC. Restart by setting false.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// envOverride adds instance-specific env vars on top of the template's.
	// A key defined here wins over template env and config-mapped env.
	// +optional
	EnvOverride []corev1.EnvVar `json:"envOverride,omitempty"`
}

// GameServerStatus reports observed state.
type GameServerStatus struct {
	// phase is a coarse summary; conditions carry detail.
	// +optional
	Phase GameServerPhase `json:"phase,omitempty"`

	// address is the resolved connect string (host:port) for the primary
	// port. Empty until a Service address is known.
	// +optional
	Address string `json:"address,omitempty"`

	// observedGeneration reflects the .metadata.generation this status
	// corresponds to.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// templateGeneration is the .metadata.generation of the referenced
	// GameTemplate at the last successful reconcile. Used to detect drift.
	// +optional
	TemplateGeneration int64 `json:"templateGeneration,omitempty"`

	// readyReplicas is 1 when the pod is Ready, 0 otherwise.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=gsv
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.templateRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.status.address`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GameServer is a single running game server instance backed by a Pod
// (Deployment), PVC, and one or more Services.
type GameServer struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec GameServerSpec `json:"spec"`
	// +optional
	Status GameServerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// GameServerList contains a list of GameServer.
type GameServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []GameServer `json:"items"`
}

// Condition type constants used across both resources.
const (
	// ConditionReady mirrors phase=Ready. True when the underlying pod is
	// Ready AND (later) the game-specific probe passes.
	ConditionReady = "Ready"
	// ConditionTemplateResolved is True once the referenced GameTemplate
	// has been fetched and validated.
	ConditionTemplateResolved = "TemplateResolved"
	// ConditionConfigValid is True when .spec.config satisfies the
	// template's configKeys constraints.
	ConditionConfigValid = "ConfigValid"
)

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &GameServer{}, &GameServerList{})
		return nil
	})
}
