/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// BackupPhase mirrors the underlying VolumeSnapshot lifecycle.
// +kubebuilder:validation:Enum=Pending;InProgress;Ready;Failed;Deleted
type BackupPhase string

const (
	BackupPhasePending    BackupPhase = "Pending"
	BackupPhaseInProgress BackupPhase = "InProgress"
	BackupPhaseReady      BackupPhase = "Ready"
	BackupPhaseFailed     BackupPhase = "Failed"
	BackupPhaseDeleted    BackupPhase = "Deleted"
)

// GameServerRef points at a GameServer in the same namespace.
type GameServerRef struct {
	// name of the referenced GameServer.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// BackupSpec is the desired state of a one-shot backup.
type BackupSpec struct {
	// gameServerRef selects which GameServer's data PVC to snapshot.
	// +required
	GameServerRef GameServerRef `json:"gameServerRef"`

	// volumeSnapshotClassName picks the VolumeSnapshotClass used for the
	// underlying snapshot.storage.k8s.io/v1 VolumeSnapshot. Empty falls
	// back to the cluster's default VolumeSnapshotClass — works with any
	// CSI driver (Synology, Longhorn, Ceph, EBS, etc.); nothing here is
	// driver-specific.
	// +optional
	VolumeSnapshotClassName string `json:"volumeSnapshotClassName,omitempty"`

	// retainForever excludes this Backup from BackupSchedule retention
	// GC. Use for pre-upgrade / milestone snapshots you want to keep
	// past the schedule's `keep` window.
	// +optional
	RetainForever bool `json:"retainForever,omitempty"`
}

// BackupStatus is the observed state of a backup.
type BackupStatus struct {
	// phase is a coarse summary. Details in conditions.
	// +optional
	Phase BackupPhase `json:"phase,omitempty"`

	// volumeSnapshotName is the name of the child VolumeSnapshot in the
	// same namespace. Populated as soon as the controller creates it.
	// +optional
	VolumeSnapshotName string `json:"volumeSnapshotName,omitempty"`

	// snapshotHandle is the CSI-assigned handle for the underlying
	// storage snapshot; useful for correlation with CSI backends.
	// +optional
	SnapshotHandle string `json:"snapshotHandle,omitempty"`

	// restoreSize is the size a restored PVC would need (from the
	// underlying VolumeSnapshotContent).
	// +optional
	RestoreSize string `json:"restoreSize,omitempty"`

	// creationTime is when the VolumeSnapshot became ReadyToUse.
	// +optional
	CreationTime *metav1.Time `json:"creationTime,omitempty"`

	// observedGeneration reflects the .metadata.generation this status
	// corresponds to.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Backup condition types.
const (
	// ConditionSnapshotBound is True once the VolumeSnapshot exists and
	// is bound to a VolumeSnapshotContent.
	ConditionSnapshotBound = "SnapshotBound"
	// ConditionBackupReady is True once the snapshot is ReadyToUse.
	ConditionBackupReady = "Ready"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=gsb
// +kubebuilder:printcolumn:name="GameServer",type=string,JSONPath=`.spec.gameServerRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Snapshot",type=string,JSONPath=`.status.volumeSnapshotName`
// +kubebuilder:printcolumn:name="Size",type=string,JSONPath=`.status.restoreSize`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Backup captures a point-in-time snapshot of a GameServer's data PVC
// via the Kubernetes VolumeSnapshot API.
type Backup struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec BackupSpec `json:"spec"`
	// +optional
	Status BackupStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// BackupList contains a list of Backup.
type BackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Backup `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Backup{}, &BackupList{})
		return nil
	})
}
