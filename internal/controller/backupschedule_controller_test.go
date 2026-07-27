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

var _ = Describe("BackupSchedule Controller", func() {
	Context("When the schedule is valid", func() {
		const (
			name      = "sched-valid"
			namespace = "default"
		)
		ctx := context.Background()
		key := types.NamespacedName{Name: name, Namespace: namespace}

		BeforeEach(func() {
			bs := &gameserverv1alpha1.BackupSchedule{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: gameserverv1alpha1.BackupScheduleSpec{
					GameServerRef: gameserverv1alpha1.GameServerRef{Name: "minecraft"},
					Schedule:      "0 4 * * *",
					Keep:          3,
					Suspend:       true, // don't actually spawn during the test
				},
			}
			existing := &gameserverv1alpha1.BackupSchedule{}
			if err := k8sClient.Get(ctx, key, existing); err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, bs)).To(Succeed())
			}
		})

		AfterEach(func() {
			bs := &gameserverv1alpha1.BackupSchedule{}
			if err := k8sClient.Get(ctx, key, bs); err == nil {
				Expect(k8sClient.Delete(ctx, bs)).To(Succeed())
			}
		})

		It("sets ScheduleValid=True and doesn't error", func() {
			r := &BackupScheduleReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			var bs gameserverv1alpha1.BackupSchedule
			Expect(k8sClient.Get(ctx, key, &bs)).To(Succeed())
			found := false
			for _, c := range bs.Status.Conditions {
				if c.Type == "ScheduleValid" && c.Status == metav1.ConditionTrue {
					found = true
				}
			}
			Expect(found).To(BeTrue())
		})
	})
})
