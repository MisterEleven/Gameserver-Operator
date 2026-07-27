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
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

// GameServerReconciler reconciles a GameServer, materializing its owned
// PVC, Deployment, and Services and reporting status.
type GameServerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=gameservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=gameservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=gameservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=gametemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *GameServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var gs gameserverv1alpha1.GameServer
	if err := r.Get(ctx, req.NamespacedName, &gs); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch referenced template (cluster-scoped).
	var tpl gameserverv1alpha1.GameTemplate
	if err := r.Get(ctx, types.NamespacedName{Name: gs.Spec.TemplateRef.Name}, &tpl); err != nil {
		if apierrors.IsNotFound(err) {
			r.setCondition(&gs, gameserverv1alpha1.ConditionTemplateResolved, metav1.ConditionFalse,
				"NotFound", fmt.Sprintf("GameTemplate %q not found", gs.Spec.TemplateRef.Name))
			gs.Status.Phase = gameserverv1alpha1.PhasePending
			if statusErr := r.updateStatus(ctx, &gs); statusErr != nil {
				log.Error(statusErr, "status update failed")
			}
			// requeue in case template is created after
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}
	r.setCondition(&gs, gameserverv1alpha1.ConditionTemplateResolved, metav1.ConditionTrue, "Resolved", "GameTemplate resolved")

	// Validate config against template.
	if err := ValidateConfig(&gs, &tpl); err != nil {
		r.setCondition(&gs, gameserverv1alpha1.ConditionConfigValid, metav1.ConditionFalse, "Invalid", err.Error())
		gs.Status.Phase = gameserverv1alpha1.PhaseDegraded
		if statusErr := r.updateStatus(ctx, &gs); statusErr != nil {
			log.Error(statusErr, "status update failed")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	r.setCondition(&gs, gameserverv1alpha1.ConditionConfigValid, metav1.ConditionTrue, "Valid", "config satisfies template")

	// Ensure ServiceAccount exists (restricted profile). anyuid SA is
	// assumed pre-created by cluster-admin.
	if tpl.Spec.SecurityProfile != gameserverv1alpha1.SecurityProfileAnyUID {
		if err := r.ensureServiceAccount(ctx, &gs); err != nil {
			return ctrl.Result{}, err
		}
	}

	// PVC. If .spec.restoreFrom is set AND the PVC doesn't exist yet,
	// resolve the referenced Backup to a VolumeSnapshot name and pass
	// it into BuildPVC so kube provisions the volume seeded from that
	// snapshot. Once the PVC exists, restoreFrom is inert — the world
	// is what it is.
	sourceSnapshot, restoreErr := r.resolveRestoreSource(ctx, &gs)
	if restoreErr != nil {
		r.setCondition(&gs, gameserverv1alpha1.ConditionRestoreSourceResolved,
			metav1.ConditionFalse, "Unavailable", restoreErr.Error())
		gs.Status.Phase = gameserverv1alpha1.PhaseDegraded
		if statusErr := r.updateStatus(ctx, &gs); statusErr != nil {
			log.Error(statusErr, "status update failed")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if gs.Spec.RestoreFrom != nil {
		r.setCondition(&gs, gameserverv1alpha1.ConditionRestoreSourceResolved,
			metav1.ConditionTrue, "SnapshotBound", "restoreFrom Backup snapshot ready")
	}

	desiredPVC, err := BuildPVC(&gs, &tpl, sourceSnapshot)
	if err != nil {
		r.setCondition(&gs, gameserverv1alpha1.ConditionConfigValid, metav1.ConditionFalse, "InvalidStorage", err.Error())
		return ctrl.Result{}, r.updateStatus(ctx, &gs)
	}
	if err := ctrl.SetControllerReference(&gs, desiredPVC, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.applyPVC(ctx, desiredPVC); err != nil {
		return ctrl.Result{}, err
	}

	// Deployment.
	desiredDep := BuildDeployment(&gs, &tpl)
	if err := ctrl.SetControllerReference(&gs, desiredDep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.applyDeployment(ctx, desiredDep); err != nil {
		return ctrl.Result{}, err
	}

	// Services.
	desiredSvcs := BuildServices(&gs, &tpl)
	appliedSvcs := make([]*corev1.Service, 0, len(desiredSvcs))
	for _, svc := range desiredSvcs {
		if err := ctrl.SetControllerReference(&gs, svc, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		applied, err := r.applyService(ctx, svc)
		if err != nil {
			return ctrl.Result{}, err
		}
		appliedSvcs = append(appliedSvcs, applied)
	}

	// Refresh Deployment status for phase/readiness.
	var current appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: DeploymentName(&gs), Namespace: gs.Namespace}, &current); err != nil {
		return ctrl.Result{}, err
	}

	gs.Status.ReadyReplicas = current.Status.ReadyReplicas
	gs.Status.ObservedGeneration = gs.Generation
	gs.Status.TemplateGeneration = tpl.Generation
	gs.Status.Address = PrimaryAddress(&gs, &tpl, appliedSvcs)

	switch {
	case gs.Spec.Suspend:
		gs.Status.Phase = gameserverv1alpha1.PhaseStopped
		r.setCondition(&gs, gameserverv1alpha1.ConditionReady, metav1.ConditionFalse, "Suspended", "spec.suspend=true")
	case current.Status.ReadyReplicas >= 1:
		gs.Status.Phase = gameserverv1alpha1.PhaseReady
		r.setCondition(&gs, gameserverv1alpha1.ConditionReady, metav1.ConditionTrue, "PodReady", "pod is Ready")
	default:
		gs.Status.Phase = gameserverv1alpha1.PhaseProvisioning
		r.setCondition(&gs, gameserverv1alpha1.ConditionReady, metav1.ConditionFalse, "PodNotReady", "waiting for pod to become Ready")
	}

	if err := r.updateStatus(ctx, &gs); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue slowly to catch NodePort / LoadBalancer address arrival.
	if gs.Status.Phase != gameserverv1alpha1.PhaseReady {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// resolveRestoreSource looks at .spec.restoreFrom and returns the
// VolumeSnapshot name to pass into BuildPVC's dataSourceRef, or nil if
// no restore is requested. Called on every reconcile but only produces
// a non-nil result when the PVC doesn't yet exist — once the PVC is
// there, restoreFrom is a no-op (PVC.spec.dataSourceRef is immutable
// anyway).
func (r *GameServerReconciler) resolveRestoreSource(ctx context.Context, gs *gameserverv1alpha1.GameServer) (*string, error) {
	if gs.Spec.RestoreFrom == nil {
		return nil, nil
	}
	// If the PVC already exists, restoreFrom is inert — don't set
	// dataSourceRef on the desired object or it'll churn.
	var existing corev1.PersistentVolumeClaim
	err := r.Get(ctx, types.NamespacedName{Name: PVCName(gs), Namespace: gs.Namespace}, &existing)
	if err == nil {
		return nil, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}

	var backup gameserverv1alpha1.Backup
	if err := r.Get(ctx, types.NamespacedName{Name: gs.Spec.RestoreFrom.BackupName, Namespace: gs.Namespace}, &backup); err != nil {
		return nil, fmt.Errorf("restoreFrom.backupName %q: %w", gs.Spec.RestoreFrom.BackupName, err)
	}
	if backup.Status.Phase != gameserverv1alpha1.BackupPhaseReady {
		return nil, fmt.Errorf("restoreFrom.backupName %q: Backup phase is %q, want Ready", backup.Name, backup.Status.Phase)
	}
	if backup.Status.VolumeSnapshotName == "" {
		return nil, fmt.Errorf("restoreFrom.backupName %q: Backup has no VolumeSnapshotName in status", backup.Name)
	}
	snap := backup.Status.VolumeSnapshotName
	return &snap, nil
}

// applyPVC creates the PVC if missing. PVCs are largely immutable so we do
// not attempt to update spec fields; only labels are patched on drift.
func (r *GameServerReconciler) applyPVC(ctx context.Context, desired *corev1.PersistentVolumeClaim) error {
	var existing corev1.PersistentVolumeClaim
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(existing.Labels, desired.Labels) {
		existing.Labels = desired.Labels
		return r.Update(ctx, &existing)
	}
	return nil
}

// applyDeployment creates or updates the Deployment. On update we replace
// spec wholesale — the reconciler is the single source of truth.
func (r *GameServerReconciler) applyDeployment(ctx context.Context, desired *appsv1.Deployment) error {
	var existing appsv1.Deployment
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.Labels = desired.Labels
	existing.Spec = desired.Spec
	return r.Update(ctx, &existing)
}

// applyService creates or updates a Service. Preserves clusterIP and
// health/session fields on update since those are set by the apiserver.
// Returns the applied (post-Get) Service so status can read assigned values.
func (r *GameServerReconciler) applyService(ctx context.Context, desired *corev1.Service) (*corev1.Service, error) {
	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return nil, err
		}
		// re-read to pick up apiserver-assigned ClusterIP / NodePorts
		if err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing); err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if err != nil {
		return nil, err
	}
	existing.Labels = desired.Labels
	existing.Spec.Type = desired.Spec.Type
	existing.Spec.Selector = desired.Spec.Selector
	// preserve NodePort / ClusterIP assignments when the port already has them
	existing.Spec.Ports = mergeServicePorts(existing.Spec.Ports, desired.Spec.Ports)
	if err := r.Update(ctx, &existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

// mergeServicePorts keeps apiserver-assigned NodePort values across updates
// unless the desired port has an explicit override.
func mergeServicePorts(existing, desired []corev1.ServicePort) []corev1.ServicePort {
	byName := map[string]corev1.ServicePort{}
	for _, p := range existing {
		byName[p.Name] = p
	}
	out := make([]corev1.ServicePort, 0, len(desired))
	for _, d := range desired {
		if e, ok := byName[d.Name]; ok && d.NodePort == 0 {
			d.NodePort = e.NodePort
		}
		out = append(out, d)
	}
	return out
}

// ensureServiceAccount idempotently creates the SA the game pod runs under.
// On OpenShift the default SCC (`restricted-v2`) admits pods using any SA
// in the namespace, so no SCC binding is needed for the restricted profile.
func (r *GameServerReconciler) ensureServiceAccount(ctx context.Context, gs *gameserverv1alpha1.GameServer) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultServiceAccountName,
			Namespace: gs.Namespace,
			Labels: map[string]string{
				labelName:      appName,
				labelManagedBy: managedByValue,
			},
		},
	}
	var existing corev1.ServiceAccount
	err := r.Get(ctx, client.ObjectKeyFromObject(sa), &existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, sa)
	}
	return err
}

func (r *GameServerReconciler) setCondition(gs *gameserverv1alpha1.GameServer, condType string, status metav1.ConditionStatus, reason, msg string) {
	now := metav1.Now()
	for i, c := range gs.Status.Conditions {
		if c.Type == condType {
			if c.Status != status {
				gs.Status.Conditions[i].LastTransitionTime = now
			}
			gs.Status.Conditions[i].Status = status
			gs.Status.Conditions[i].Reason = reason
			gs.Status.Conditions[i].Message = msg
			gs.Status.Conditions[i].ObservedGeneration = gs.Generation
			return
		}
	}
	gs.Status.Conditions = append(gs.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: now,
		ObservedGeneration: gs.Generation,
	})
}

func (r *GameServerReconciler) updateStatus(ctx context.Context, gs *gameserverv1alpha1.GameServer) error {
	return r.Status().Update(ctx, gs)
}

// SetupWithManager wires the reconciler and watches for owned resources
// plus GameTemplate changes (which fan out to affected GameServers).
func (r *GameServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserverv1alpha1.GameServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Watches(
			&gameserverv1alpha1.GameTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.mapTemplateToServers),
			builder.WithPredicates(),
		).
		Named("gameserver").
		Complete(r)
}

// mapTemplateToServers finds every GameServer that references the given
// template and enqueues each for reconcile. This is how template changes
// propagate without polling.
func (r *GameServerReconciler) mapTemplateToServers(ctx context.Context, obj client.Object) []reconcile.Request {
	tpl, ok := obj.(*gameserverv1alpha1.GameTemplate)
	if !ok {
		return nil
	}
	var list gameserverv1alpha1.GameServerList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0)
	for _, gs := range list.Items {
		if gs.Spec.TemplateRef.Name == tpl.Name {
			out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Name: gs.Name, Namespace: gs.Namespace}})
		}
	}
	return out
}

// suppress unused import warning while controllerutil is not yet used
// directly by name (SetControllerReference is called via ctrl alias).
var _ = controllerutil.SetControllerReference
