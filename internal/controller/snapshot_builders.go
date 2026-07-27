/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

// VolumeSnapshotName is the deterministic name of the VolumeSnapshot
// owned by a Backup. Mirrors the Backup's name for easy correlation.
func VolumeSnapshotName(b *gameserverv1alpha1.Backup) string {
	return b.Name
}

// BuildVolumeSnapshot materializes the VolumeSnapshot the Backup owns.
// Uses snapshot.storage.k8s.io/v1 — CSI-agnostic; the actual snapshot
// mechanics come from whatever driver backs the VolumeSnapshotClass.
func BuildVolumeSnapshot(b *gameserverv1alpha1.Backup, gs *gameserverv1alpha1.GameServer) *snapshotv1.VolumeSnapshot {
	labels := map[string]string{
		labelName:      appName,
		labelInstance:  gs.Name,
		labelManagedBy: managedByValue,
		labelBackup:    b.Name,
	}
	pvcName := PVCName(gs)
	vs := &snapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VolumeSnapshotName(b),
			Namespace: b.Namespace,
			Labels:    labels,
		},
		Spec: snapshotv1.VolumeSnapshotSpec{
			Source: snapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: &pvcName,
			},
		},
	}
	if class := b.Spec.VolumeSnapshotClassName; class != "" {
		vs.Spec.VolumeSnapshotClassName = &class
	}
	return vs
}
