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
	"slices"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

// BackupScheduleReconciler stamps out Backup objects on a cron
// schedule and applies keep-N retention.
type BackupScheduleReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Now is injected for tests; production uses time.Now.
	Now func() time.Time
}

// scheduleParser is stdlib-cron-compatible (5-field, no seconds).
// Matches batch/v1 CronJob semantics.
var scheduleParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=backupschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=backupschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=backupschedules/finalizers,verbs=update
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=backups,verbs=get;list;watch;create;update;patch;delete

func (r *BackupScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	now := r.now()

	var bs gameserverv1alpha1.BackupSchedule
	if err := r.Get(ctx, req.NamespacedName, &bs); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	loc, err := loadLocation(bs.Spec.TimeZone)
	if err != nil {
		setScheduleCondition(&bs, "ScheduleValid", metav1.ConditionFalse, "InvalidTimeZone", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, r.Status().Update(ctx, &bs)
	}
	sched, err := scheduleParser.Parse(bs.Spec.Schedule)
	if err != nil {
		setScheduleCondition(&bs, "ScheduleValid", metav1.ConditionFalse, "InvalidSchedule", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, r.Status().Update(ctx, &bs)
	}
	setScheduleCondition(&bs, "ScheduleValid", metav1.ConditionTrue, "Parsed", "cron parsed cleanly")

	// Reference point for "last time we should have fired": the more
	// recent of lastScheduleTime and (now - 24h). The 24h floor keeps
	// us from spamming missed runs after a long controller downtime.
	ref := now.Add(-24 * time.Hour)
	if bs.Status.LastScheduleTime != nil && bs.Status.LastScheduleTime.After(ref) {
		ref = bs.Status.LastScheduleTime.Time
	}
	nextFire := sched.Next(ref.In(loc))

	// Fire if due and not suspended.
	if !bs.Spec.Suspend && !now.Before(nextFire) {
		if err := r.spawnBackup(ctx, &bs, nextFire); err != nil {
			log.Error(err, "spawn Backup failed")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		bs.Status.LastScheduleTime = &metav1.Time{Time: nextFire}
	}

	// Retention: list all our Backups, sorted newest-first, retain first
	// `keep` non-retainForever, delete the rest.
	if err := r.applyRetention(ctx, &bs); err != nil {
		log.Error(err, "retention pass failed")
	}

	// Refresh activeBackups + lastSuccessfulTime from children.
	if err := r.refreshChildStatus(ctx, &bs); err != nil {
		log.Error(err, "child refresh failed")
	}

	bs.Status.ObservedGeneration = bs.Generation
	if err := r.Status().Update(ctx, &bs); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue right after the next scheduled time — the min just
	// ensures we don't drift more than ~1min from any wall-clock skew.
	waitFor := max(time.Until(sched.Next(now.In(loc))), time.Minute)
	return ctrl.Result{RequeueAfter: waitFor}, nil
}

func (r *BackupScheduleReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func loadLocation(tz string) (*time.Location, error) {
	if tz == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("timeZone %q: %w", tz, err)
	}
	return loc, nil
}

// spawnBackup creates a Backup CR owned by this BackupSchedule. Name
// includes the schedule's name and the target fire time; unique-enough
// for a homelab cadence.
func (r *BackupScheduleReconciler) spawnBackup(ctx context.Context, bs *gameserverv1alpha1.BackupSchedule, fireTime time.Time) error {
	b := &gameserverv1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", bs.Name, fireTime.UTC().Format("20060102-1504")),
			Namespace: bs.Namespace,
			Labels: map[string]string{
				labelName:      appName,
				labelInstance:  bs.Spec.GameServerRef.Name,
				labelManagedBy: managedByValue,
				labelSchedule:  bs.Name,
			},
		},
		Spec: gameserverv1alpha1.BackupSpec{
			GameServerRef:           bs.Spec.GameServerRef,
			VolumeSnapshotClassName: bs.Spec.VolumeSnapshotClassName,
		},
	}
	if err := ctrl.SetControllerReference(bs, b, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, b); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// applyRetention deletes Backups past `keep`, oldest-first. Skips any
// Backup with retainForever=true.
func (r *BackupScheduleReconciler) applyRetention(ctx context.Context, bs *gameserverv1alpha1.BackupSchedule) error {
	var list gameserverv1alpha1.BackupList
	if err := r.List(ctx, &list, client.InNamespace(bs.Namespace),
		client.MatchingLabels{labelSchedule: bs.Name}); err != nil {
		return err
	}
	// Newest first.
	slices.SortFunc(list.Items, func(a, b gameserverv1alpha1.Backup) int {
		return b.CreationTimestamp.Compare(a.CreationTimestamp.Time)
	})

	keep := int(bs.Spec.Keep)
	if keep <= 0 {
		keep = 7 // matches the CRD default
	}
	kept := 0
	for i := range list.Items {
		b := &list.Items[i]
		if b.Spec.RetainForever {
			continue
		}
		if kept < keep {
			kept++
			continue
		}
		if err := r.Delete(ctx, b); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// refreshChildStatus recomputes .status.activeBackups and lastSuccessfulTime.
func (r *BackupScheduleReconciler) refreshChildStatus(ctx context.Context, bs *gameserverv1alpha1.BackupSchedule) error {
	var list gameserverv1alpha1.BackupList
	if err := r.List(ctx, &list, client.InNamespace(bs.Namespace),
		client.MatchingLabels{labelSchedule: bs.Name}); err != nil {
		return err
	}
	active := []corev1.LocalObjectReference{}
	var lastSuccess *metav1.Time
	for i := range list.Items {
		b := &list.Items[i]
		switch b.Status.Phase {
		case gameserverv1alpha1.BackupPhaseReady:
			if b.Status.CreationTime != nil && (lastSuccess == nil || b.Status.CreationTime.After(lastSuccess.Time)) {
				lastSuccess = b.Status.CreationTime
			}
		case gameserverv1alpha1.BackupPhasePending, gameserverv1alpha1.BackupPhaseInProgress:
			active = append(active, corev1.LocalObjectReference{Name: b.Name})
		}
	}
	bs.Status.ActiveBackups = active
	if lastSuccess != nil {
		bs.Status.LastSuccessfulTime = lastSuccess
	}
	return nil
}

func setScheduleCondition(bs *gameserverv1alpha1.BackupSchedule, condType string, status metav1.ConditionStatus, reason, msg string) {
	now := metav1.Now()
	for i, c := range bs.Status.Conditions {
		if c.Type == condType {
			if c.Status != status {
				bs.Status.Conditions[i].LastTransitionTime = now
			}
			bs.Status.Conditions[i].Status = status
			bs.Status.Conditions[i].Reason = reason
			bs.Status.Conditions[i].Message = msg
			bs.Status.Conditions[i].ObservedGeneration = bs.Generation
			return
		}
	}
	bs.Status.Conditions = append(bs.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: now,
		ObservedGeneration: bs.Generation,
	})
}

// SetupWithManager watches for child Backups so that status changes
// (Ready, Failed) enqueue a schedule reconcile — retention and
// activeBackups stay fresh without polling.
func (r *BackupScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserverv1alpha1.BackupSchedule{}).
		Owns(&gameserverv1alpha1.Backup{}).
		Watches(
			&gameserverv1alpha1.Backup{},
			handler.EnqueueRequestsFromMapFunc(r.mapBackupToSchedule),
			builder.WithPredicates(),
		).
		Named("backupschedule").
		Complete(r)
}

// mapBackupToSchedule pulls the schedule name off a Backup's labels and
// enqueues the corresponding BackupSchedule. Complements ownerRef watch
// (Owns) for Backups created out-of-band with the schedule label.
func (r *BackupScheduleReconciler) mapBackupToSchedule(_ context.Context, obj client.Object) []reconcile.Request {
	b, ok := obj.(*gameserverv1alpha1.Backup)
	if !ok {
		return nil
	}
	name := b.Labels[labelSchedule]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: name, Namespace: b.Namespace}}}
}
