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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

var _ = Describe("Backup Controller", func() {
	Context("When the referenced GameServer is missing", func() {
		const (
			backupName = "missing-gs-backup"
			namespace  = "default"
		)
		ctx := context.Background()
		key := types.NamespacedName{Name: backupName, Namespace: namespace}

		BeforeEach(func() {
			b := &gameserverv1alpha1.Backup{
				ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: namespace},
				Spec: gameserverv1alpha1.BackupSpec{
					GameServerRef: gameserverv1alpha1.GameServerRef{Name: "nonexistent"},
				},
			}
			existing := &gameserverv1alpha1.Backup{}
			if err := k8sClient.Get(ctx, key, existing); err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, b)).To(Succeed())
			}
		})

		AfterEach(func() {
			b := &gameserverv1alpha1.Backup{}
			if err := k8sClient.Get(ctx, key, b); err == nil {
				Expect(k8sClient.Delete(ctx, b)).To(Succeed())
			}
		})

		It("marks Backup phase=Pending without erroring", func() {
			r := &BackupReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			var b gameserverv1alpha1.Backup
			Expect(k8sClient.Get(ctx, key, &b)).To(Succeed())
			Expect(b.Status.Phase).To(Equal(gameserverv1alpha1.BackupPhasePending))
		})
	})
})
