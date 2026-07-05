/*
Copyright 2026 Tim Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

// GameTemplateReconciler keeps GameTemplate.status in sync — mainly a
// counter of how many GameServers reference it. The heavier lifting of
// re-reconciling servers on template change lives on GameServerReconciler.
type GameTemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=gametemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=gametemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gameserver.feddern.dev,resources=gametemplates/finalizers,verbs=update

func (r *GameTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var tpl gameserverv1alpha1.GameTemplate
	if err := r.Get(ctx, req.NamespacedName, &tpl); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	var servers gameserverv1alpha1.GameServerList
	if err := r.List(ctx, &servers); err != nil {
		return ctrl.Result{}, err
	}
	var count int32
	for _, s := range servers.Items {
		if s.Spec.TemplateRef.Name == tpl.Name {
			count++
		}
	}

	if tpl.Status.ServersRegistered == count && hasAvailableCondition(&tpl) {
		return ctrl.Result{}, nil
	}

	tpl.Status.ServersRegistered = count
	setTemplateCondition(&tpl, metav1.ConditionTrue, "Registered", "template observed")

	if err := r.Status().Update(ctx, &tpl); err != nil {
		log.Error(err, "template status update failed")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func setTemplateCondition(tpl *gameserverv1alpha1.GameTemplate, status metav1.ConditionStatus, reason, msg string) {
	now := metav1.Now()
	for i, c := range tpl.Status.Conditions {
		if c.Type != "Available" {
			continue
		}
		if c.Status != status {
			tpl.Status.Conditions[i].LastTransitionTime = now
		}
		tpl.Status.Conditions[i].Status = status
		tpl.Status.Conditions[i].Reason = reason
		tpl.Status.Conditions[i].Message = msg
		tpl.Status.Conditions[i].ObservedGeneration = tpl.Generation
		return
	}
	tpl.Status.Conditions = append(tpl.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: now,
		ObservedGeneration: tpl.Generation,
	})
}

func hasAvailableCondition(tpl *gameserverv1alpha1.GameTemplate) bool {
	for _, c := range tpl.Status.Conditions {
		if c.Type == "Available" && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// mapServerToTemplate enqueues a template reconcile whenever a GameServer
// referencing it is created/updated/deleted so the count stays fresh.
func (r *GameTemplateReconciler) mapServerToTemplate(_ context.Context, obj client.Object) []reconcile.Request {
	gs, ok := obj.(*gameserverv1alpha1.GameServer)
	if !ok || gs.Spec.TemplateRef.Name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: gs.Spec.TemplateRef.Name}}}
}

// SetupWithManager wires the reconciler and watches GameServers to keep
// serversRegistered fresh.
func (r *GameTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gameserverv1alpha1.GameTemplate{}).
		Watches(
			&gameserverv1alpha1.GameServer{},
			handler.EnqueueRequestsFromMapFunc(r.mapServerToTemplate),
			builder.WithPredicates(),
		).
		Named("gametemplate").
		Complete(r)
}
