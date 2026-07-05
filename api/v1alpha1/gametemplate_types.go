/*
Copyright 2026 Tim Feddern.

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

// SecurityProfile selects how strict a game pod's securityContext must be.
// restricted works under OpenShift restricted-v2 SCC and plain K8s.
// anyuid is an opt-in escape hatch for images that cannot run as an
// arbitrary UID; on OpenShift it requires a cluster-admin-bound SA.
// +kubebuilder:validation:Enum=restricted;anyuid
type SecurityProfile string

const (
	SecurityProfileRestricted SecurityProfile = "restricted"
	SecurityProfileAnyUID     SecurityProfile = "anyuid"
)

// ExposureType controls how a template's port is surfaced.
// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
type ExposureType string

const (
	ExposureClusterIP    ExposureType = "ClusterIP"
	ExposureNodePort     ExposureType = "NodePort"
	ExposureLoadBalancer ExposureType = "LoadBalancer"
)

// UpdateStrategyKind mirrors appsv1.DeploymentStrategyType but constrained.
// Recreate is the safe default for RWO PVC-backed servers.
// +kubebuilder:validation:Enum=Recreate;RollingUpdate
type UpdateStrategyKind string

const (
	UpdateRecreate      UpdateStrategyKind = "Recreate"
	UpdateRollingUpdate UpdateStrategyKind = "RollingUpdate"
)

// TemplatePort describes one exposed port for a game.
type TemplatePort struct {
	// name is a short identifier for this port (e.g. "game", "rcon", "query").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +required
	Name string `json:"name"`

	// containerPort is the port the game process listens on inside the pod.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +required
	ContainerPort int32 `json:"containerPort"`

	// protocol is TCP or UDP. Defaults to TCP.
	// +kubebuilder:validation:Enum=TCP;UDP
	// +kubebuilder:default=TCP
	// +optional
	Protocol corev1.Protocol `json:"protocol,omitempty"`

	// exposeAs selects the Service type used for this port.
	// Defaults to ClusterIP; templates that need external reachability should
	// set NodePort or LoadBalancer explicitly.
	// +kubebuilder:default=ClusterIP
	// +optional
	ExposeAs ExposureType `json:"exposeAs,omitempty"`

	// primary marks the port used to compute .status.address when a GameServer
	// has multiple ports. First TCP port wins if none is marked primary.
	// +optional
	Primary bool `json:"primary,omitempty"`
}

// TemplateStorage describes how the persistent data volume is laid out
// inside the pod. Actual PVC size/class comes from the GameServer.
type TemplateStorage struct {
	// dataPath is the mount point inside the container for persistent data.
	// +kubebuilder:default="/data"
	// +optional
	DataPath string `json:"dataPath,omitempty"`

	// subPath optionally scopes the mount to a subdirectory of the PVC.
	// +optional
	SubPath string `json:"subPath,omitempty"`

	// defaultSize is the PVC size when a GameServer does not override it.
	// String form because resource.Quantity requires it (e.g. "10Gi").
	// +kubebuilder:default="10Gi"
	// +optional
	DefaultSize string `json:"defaultSize,omitempty"`
}

// ConfigKey declares one knob a GameServer may set under .spec.config.
// Every key defined here becomes a container env var (name = envVar).
// Constraints follow the JSON Schema subset supported by CRD validation.
type ConfigKey struct {
	// name is the key GameServers reference in .spec.config.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// envVar is the container env variable this key maps to.
	// +kubebuilder:validation:MinLength=1
	// +required
	EnvVar string `json:"envVar"`

	// description is shown to users authoring GameServers.
	// +optional
	Description string `json:"description,omitempty"`

	// type constrains accepted values. Defaults to string.
	// +kubebuilder:validation:Enum=string;int;bool;enum
	// +kubebuilder:default=string
	// +optional
	Type string `json:"type,omitempty"`

	// enum lists the allowed values when type=enum.
	// +optional
	Enum []string `json:"enum,omitempty"`

	// default is the value used when a GameServer omits this key.
	// +optional
	Default string `json:"default,omitempty"`

	// required marks the key as mandatory; GameServer without it fails
	// validation in the reconciler (and, later, the webhook).
	// +optional
	Required bool `json:"required,omitempty"`
}

// GameTemplateSpec defines the desired state of a reusable game template.
type GameTemplateSpec struct {
	// image is the OCI image the game pod will run.
	// +kubebuilder:validation:MinLength=1
	// +required
	Image string `json:"image"`

	// imagePullPolicy defaults to IfNotPresent.
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +kubebuilder:default=IfNotPresent
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// ports declares which ports the game exposes.
	// +kubebuilder:validation:MinItems=1
	// +required
	Ports []TemplatePort `json:"ports"`

	// env is applied to every game pod unconditionally. Per-instance values
	// come via .spec.config and configKeys mapping.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// configKeys declares the knobs GameServers may set under .spec.config.
	// +optional
	// +listType=map
	// +listMapKey=name
	ConfigKeys []ConfigKey `json:"configKeys,omitempty"`

	// command overrides the container entrypoint.
	// +optional
	Command []string `json:"command,omitempty"`

	// args overrides the container args.
	// +optional
	Args []string `json:"args,omitempty"`

	// storage describes the persistent data volume layout.
	// +optional
	Storage TemplateStorage `json:"storage,omitempty"`

	// probes is a container-level probe spec used verbatim when set.
	// A future release will add per-game protocol probes; until then the
	// controller falls back to a TCP probe on the primary port.
	// +optional
	Probes *ProbeSpec `json:"probes,omitempty"`

	// updateStrategy selects the Deployment strategy. Recreate is the safe
	// default because most game PVCs are RWO.
	// +kubebuilder:default=Recreate
	// +optional
	UpdateStrategy UpdateStrategyKind `json:"updateStrategy,omitempty"`

	// securityProfile chooses how strict the pod's securityContext is.
	// +kubebuilder:default=restricted
	// +optional
	SecurityProfile SecurityProfile `json:"securityProfile,omitempty"`
}

// ProbeSpec bundles liveness and readiness probes; either may be omitted.
type ProbeSpec struct {
	// +optional
	Liveness *corev1.Probe `json:"liveness,omitempty"`
	// +optional
	Readiness *corev1.Probe `json:"readiness,omitempty"`
}

// GameTemplateStatus reports observed state.
type GameTemplateStatus struct {
	// serversRegistered is the number of GameServers currently referencing
	// this template. Updated by the template reconciler.
	// +optional
	ServersRegistered int32 `json:"serversRegistered,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=gtpl
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.securityProfile`
// +kubebuilder:printcolumn:name="Servers",type=integer,JSONPath=`.status.serversRegistered`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GameTemplate is a reusable, cluster-scoped definition for a class of
// game server (e.g. Minecraft, Valheim). GameServers reference it.
type GameTemplate struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec GameTemplateSpec `json:"spec"`
	// +optional
	Status GameTemplateStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// GameTemplateList contains a list of GameTemplate.
type GameTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []GameTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &GameTemplate{}, &GameTemplateList{})
		return nil
	})
}
