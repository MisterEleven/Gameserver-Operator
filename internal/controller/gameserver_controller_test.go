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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

var _ = Describe("GameServer Controller", func() {
	Context("When reconciling a resource with a valid template", func() {
		const (
			templateName = "minecraft-test"
			serverName   = "test-server"
			namespace    = "default"
		)

		ctx := context.Background()
		serverKey := types.NamespacedName{Name: serverName, Namespace: namespace}
		templateKey := types.NamespacedName{Name: templateName}

		BeforeEach(func() {
			tpl := &gameserverv1alpha1.GameTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: templateName},
				Spec: gameserverv1alpha1.GameTemplateSpec{
					Image: "itzg/minecraft-server:latest",
					Ports: []gameserverv1alpha1.TemplatePort{{
						Name:          gameContainerName,
						ContainerPort: 25565,
						Protocol:      corev1.ProtocolTCP,
						ExposeAs:      gameserverv1alpha1.ExposureClusterIP,
						Primary:       true,
					}},
					Storage:         gameserverv1alpha1.TemplateStorage{DefaultSize: "1Gi"},
					SecurityProfile: gameserverv1alpha1.SecurityProfileRestricted,
				},
			}
			existing := &gameserverv1alpha1.GameTemplate{}
			if err := k8sClient.Get(ctx, templateKey, existing); err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, tpl)).To(Succeed())
			}

			gs := &gameserverv1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{Name: serverName, Namespace: namespace},
				Spec: gameserverv1alpha1.GameServerSpec{
					TemplateRef: gameserverv1alpha1.TemplateRef{Name: templateName},
				},
			}
			existingGs := &gameserverv1alpha1.GameServer{}
			if err := k8sClient.Get(ctx, serverKey, existingGs); err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, gs)).To(Succeed())
			}
		})

		AfterEach(func() {
			gs := &gameserverv1alpha1.GameServer{}
			if err := k8sClient.Get(ctx, serverKey, gs); err == nil {
				Expect(k8sClient.Delete(ctx, gs)).To(Succeed())
			}
			tpl := &gameserverv1alpha1.GameTemplate{}
			if err := k8sClient.Get(ctx, templateKey, tpl); err == nil {
				Expect(k8sClient.Delete(ctx, tpl)).To(Succeed())
			}
		})

		It("creates the expected child objects", func() {
			r := &GameServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: serverKey})
			Expect(err).NotTo(HaveOccurred())

			var pvc corev1.PersistentVolumeClaim
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serverName + "-data", Namespace: namespace}, &pvc)).To(Succeed())

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serverName, Namespace: namespace}, &dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("itzg/minecraft-server:latest"))

			var svc corev1.Service
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serverName + "-clusterip", Namespace: namespace}, &svc)).To(Succeed())
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		})
	})
})
