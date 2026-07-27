/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"
	"time"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

// BackupReconciler creates and tracks a VolumeSnapshot for each Backup.
// The child VolumeSnapshot is owned by the Backup so DeletePropagation
// cascades correctly; its `snapshot.storage.k8s.io/v1` API is
// CSI-agnostic — any driver advertising the SNAPSHOT capability works.
type BackupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=backups/finalizers,verbs=update
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=gameservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create;update;patch;delete

func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var backup gameserverv1alpha1.Backup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Resolve source PVC via GameServer.
	var gs gameserverv1alpha1.GameServer
	if err := r.Get(ctx, types.NamespacedName{Name: backup.Spec.GameServerRef.Name, Namespace: backup.Namespace}, &gs); err != nil {
		if apierrors.IsNotFound(err) {
			backup.Status.Phase = gameserverv1alpha1.BackupPhasePending
			setBackupCondition(&backup, gameserverv1alpha1.ConditionBackupReady,
				metav1.ConditionFalse, "GameServerMissing",
				fmt.Sprintf("GameServer %q not found in namespace %q", backup.Spec.GameServerRef.Name, backup.Namespace))
			if statusErr := r.Status().Update(ctx, &backup); statusErr != nil {
				log.Error(statusErr, "status update failed")
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// Materialize the VolumeSnapshot.
	desiredVS := BuildVolumeSnapshot(&backup, &gs)
	if err := ctrl.SetControllerReference(&backup, desiredVS, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	var existingVS snapshotv1.VolumeSnapshot
	err := r.Get(ctx, client.ObjectKeyFromObject(desiredVS), &existingVS)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, desiredVS); err != nil {
			return ctrl.Result{}, err
		}
		backup.Status.VolumeSnapshotName = desiredVS.Name
		backup.Status.Phase = gameserverv1alpha1.BackupPhaseInProgress
		setBackupCondition(&backup, gameserverv1alpha1.ConditionSnapshotBound,
			metav1.ConditionFalse, "Creating", "VolumeSnapshot created; waiting for bind")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, r.Status().Update(ctx, &backup)
	case err != nil:
		return ctrl.Result{}, err
	}

	// Mirror VolumeSnapshot status → Backup status.
	backup.Status.VolumeSnapshotName = existingVS.Name
	backup.Status.ObservedGeneration = backup.Generation

	if existingVS.Status != nil {
		if existingVS.Status.BoundVolumeSnapshotContentName != nil {
			setBackupCondition(&backup, gameserverv1alpha1.ConditionSnapshotBound,
				metav1.ConditionTrue, "Bound",
				fmt.Sprintf("bound to VolumeSnapshotContent %q", *existingVS.Status.BoundVolumeSnapshotContentName))
		}
		if existingVS.Status.RestoreSize != nil {
			backup.Status.RestoreSize = existingVS.Status.RestoreSize.String()
		}
		if existingVS.Status.CreationTime != nil {
			backup.Status.CreationTime = existingVS.Status.CreationTime
		}
		if existingVS.Status.ReadyToUse != nil && *existingVS.Status.ReadyToUse {
			backup.Status.Phase = gameserverv1alpha1.BackupPhaseReady
			setBackupCondition(&backup, gameserverv1alpha1.ConditionBackupReady,
				metav1.ConditionTrue, "SnapshotReady", "VolumeSnapshot ReadyToUse")
			// Snapshot handle: only available via the bound VSC, which
			// we don't fetch here (that'd need cluster-scope RBAC on
			// VolumeSnapshotContent). Leave blank for MVP; add later
			// if operators need it for cross-cluster restore.
			return ctrl.Result{}, r.Status().Update(ctx, &backup)
		}
		if existingVS.Status.Error != nil && existingVS.Status.Error.Message != nil {
			backup.Status.Phase = gameserverv1alpha1.BackupPhaseFailed
			setBackupCondition(&backup, gameserverv1alpha1.ConditionBackupReady,
				metav1.ConditionFalse, "SnapshotError", *existingVS.Status.Error.Message)
			return ctrl.Result{}, r.Status().Update(ctx, &backup)
		}
	}

	backup.Status.Phase = gameserverv1alpha1.BackupPhaseInProgress
	setBackupCondition(&backup, gameserverv1alpha1.ConditionBackupReady,
		metav1.ConditionFalse, "WaitingForReady", "VolumeSnapshot not yet ReadyToUse")
	if err := r.Status().Update(ctx, &backup); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func setBackupCondition(b *gameserverv1alpha1.Backup, condType string, status metav1.ConditionStatus, reason, msg string) {
	now := metav1.Now()
	for i, c := range b.Status.Conditions {
		if c.Type == condType {
			if c.Status != status {
				b.Status.Conditions[i].LastTransitionTime = now
			}
			b.Status.Conditions[i].Status = status
			b.Status.Conditions[i].Reason = reason
			b.Status.Conditions[i].Message = msg
			b.Status.Conditions[i].ObservedGeneration = b.Generation
			return
		}
	}
	b.Status.Conditions = append(b.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: now,
		ObservedGeneration: b.Generation,
	})
}

// SetupWithManager wires the reconciler and watches for owned
// VolumeSnapshots so status updates trigger requeue.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserverv1alpha1.Backup{}).
		Owns(&snapshotv1.VolumeSnapshot{}).
		Named("backup").
		Complete(r)
}
