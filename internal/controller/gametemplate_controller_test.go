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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

var _ = Describe("GameTemplate Controller", func() {
	Context("When reconciling a valid template", func() {
		const name = "tpl-test"
		ctx := context.Background()
		key := types.NamespacedName{Name: name}

		BeforeEach(func() {
			tpl := &gameserverv1alpha1.GameTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec: gameserverv1alpha1.GameTemplateSpec{
					Image: "example/game:1",
					Ports: []gameserverv1alpha1.TemplatePort{{
						Name:          "game",
						ContainerPort: 1234,
						Protocol:      corev1.ProtocolTCP,
						ExposeAs:      gameserverv1alpha1.ExposureClusterIP,
					}},
				},
			}
			existing := &gameserverv1alpha1.GameTemplate{}
			if err := k8sClient.Get(ctx, key, existing); err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, tpl)).To(Succeed())
			}
		})

		AfterEach(func() {
			tpl := &gameserverv1alpha1.GameTemplate{}
			if err := k8sClient.Get(ctx, key, tpl); err == nil {
				Expect(k8sClient.Delete(ctx, tpl)).To(Succeed())
			}
		})

		It("sets Available=True and serversRegistered=0 when no servers exist", func() {
			r := &GameTemplateReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			var tpl gameserverv1alpha1.GameTemplate
			Expect(k8sClient.Get(ctx, key, &tpl)).To(Succeed())
			Expect(tpl.Status.ServersRegistered).To(Equal(int32(0)))
			found := false
			for _, c := range tpl.Status.Conditions {
				if c.Type == "Available" && c.Status == metav1.ConditionTrue {
					found = true
				}
			}
			Expect(found).To(BeTrue())
		})
	})
})
