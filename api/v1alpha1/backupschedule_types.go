/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// BackupScheduleSpec defines a cron-driven schedule that produces
// Backup resources for a GameServer.
type BackupScheduleSpec struct {
	// gameServerRef selects the GameServer to back up.
	// +required
	GameServerRef GameServerRef `json:"gameServerRef"`

	// schedule is a 5-field cron expression (minute, hour, day-of-month,
	// month, day-of-week). Interpreted in .spec.timeZone if set,
	// otherwise UTC.
	// +kubebuilder:validation:MinLength=9
	// +required
	Schedule string `json:"schedule"`

	// timeZone is an IANA time zone name (e.g. "Europe/Zurich"). Defaults
	// to UTC when unset. Matches the semantics of batch/v1 CronJob.
	// +optional
	TimeZone string `json:"timeZone,omitempty"`

	// keep is the number of most-recent successful Backups to retain
	// (per this schedule). Older ones are deleted. `retainForever: true`
	// Backups are always kept and never counted.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=7
	// +optional
	Keep int32 `json:"keep,omitempty"`

	// suspend, if true, prevents new Backups from being created but
	// leaves existing ones untouched.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// volumeSnapshotClassName is copied onto each child Backup's spec.
	// See Backup for CSI-agnostic notes.
	// +optional
	VolumeSnapshotClassName string `json:"volumeSnapshotClassName,omitempty"`
}

// BackupScheduleStatus reports schedule state.
type BackupScheduleStatus struct {
	// lastScheduleTime is when the controller last created a Backup.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// lastSuccessfulTime is when a scheduled Backup last reached Ready.
	// +optional
	LastSuccessfulTime *metav1.Time `json:"lastSuccessfulTime,omitempty"`

	// activeBackups lists Backups this schedule owns that are not yet
	// in a terminal phase.
	// +optional
	ActiveBackups []corev1.LocalObjectReference `json:"activeBackups,omitempty"`

	// observedGeneration reflects the .metadata.generation this status
	// corresponds to.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=gbs
// +kubebuilder:printcolumn:name="GameServer",type=string,JSONPath=`.spec.gameServerRef.name`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Keep",type=integer,JSONPath=`.spec.keep`
// +kubebuilder:printcolumn:name="Suspend",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="LastRun",type=date,JSONPath=`.status.lastScheduleTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// BackupSchedule creates Backups on a cron schedule and applies
// keep-N retention to its own children.
type BackupSchedule struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec BackupScheduleSpec `json:"spec"`
	// +optional
	Status BackupScheduleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// BackupScheduleList contains a list of BackupSchedule.
type BackupScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []BackupSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &BackupSchedule{}, &BackupScheduleList{})
		return nil
	})
}
