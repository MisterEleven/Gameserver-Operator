/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

func TestBuildVolumeSnapshot_MinimalFields(t *testing.T) {
	gs := &gameserverv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testNSMinecraft},
		Spec: gameserverv1alpha1.GameServerSpec{
			TemplateRef: gameserverv1alpha1.TemplateRef{Name: "minecraft-java"},
		},
	}
	b := &gameserverv1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "mc-backup", Namespace: testNSMinecraft},
		Spec:       gameserverv1alpha1.BackupSpec{GameServerRef: gameserverv1alpha1.GameServerRef{Name: "mc"}},
	}
	vs := BuildVolumeSnapshot(b, gs)
	if vs.Name != "mc-backup" {
		t.Errorf("VolumeSnapshot name = %q; want %q", vs.Name, b.Name)
	}
	if vs.Namespace != testNSMinecraft {
		t.Errorf("VolumeSnapshot namespace = %q; want %q", vs.Namespace, b.Namespace)
	}
	if vs.Spec.Source.PersistentVolumeClaimName == nil || *vs.Spec.Source.PersistentVolumeClaimName != "mc-data" {
		t.Errorf("VolumeSnapshot source pvcName mismatch: %+v", vs.Spec.Source)
	}
	if vs.Spec.VolumeSnapshotClassName != nil {
		t.Errorf("expected nil VolumeSnapshotClassName when Backup omits it, got %q", *vs.Spec.VolumeSnapshotClassName)
	}
}

func TestBuildVolumeSnapshot_ExplicitClass(t *testing.T) {
	gs := &gameserverv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testNSMinecraft},
	}
	b := &gameserverv1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "b1", Namespace: testNSMinecraft},
		Spec: gameserverv1alpha1.BackupSpec{
			GameServerRef:           gameserverv1alpha1.GameServerRef{Name: "mc"},
			VolumeSnapshotClassName: "synology-snapshot",
		},
	}
	vs := BuildVolumeSnapshot(b, gs)
	if vs.Spec.VolumeSnapshotClassName == nil || *vs.Spec.VolumeSnapshotClassName != "synology-snapshot" {
		t.Errorf("expected VolumeSnapshotClassName=synology-snapshot, got %+v", vs.Spec.VolumeSnapshotClassName)
	}
}

func TestBuildPVC_RestoreFromSnapshot(t *testing.T) {
	gs := &gameserverv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testNSMinecraft},
		Spec:       gameserverv1alpha1.GameServerSpec{TemplateRef: gameserverv1alpha1.TemplateRef{Name: "t"}},
	}
	tpl := &gameserverv1alpha1.GameTemplate{
		Spec: gameserverv1alpha1.GameTemplateSpec{
			Storage: gameserverv1alpha1.TemplateStorage{DefaultSize: "5Gi"},
		},
	}
	snap := "my-snapshot"
	pvc, err := BuildPVC(gs, tpl, &snap)
	if err != nil {
		t.Fatalf("BuildPVC err: %v", err)
	}
	if pvc.Spec.DataSourceRef == nil {
		t.Fatal("expected DataSourceRef set for restore")
	}
	if pvc.Spec.DataSourceRef.APIGroup == nil || *pvc.Spec.DataSourceRef.APIGroup != "snapshot.storage.k8s.io" {
		t.Errorf("APIGroup = %+v; want snapshot.storage.k8s.io", pvc.Spec.DataSourceRef.APIGroup)
	}
	if pvc.Spec.DataSourceRef.Kind != "VolumeSnapshot" {
		t.Errorf("Kind = %q; want VolumeSnapshot", pvc.Spec.DataSourceRef.Kind)
	}
	if pvc.Spec.DataSourceRef.Name != "my-snapshot" {
		t.Errorf("Name = %q; want my-snapshot", pvc.Spec.DataSourceRef.Name)
	}
}

func TestBuildPVC_NoRestore(t *testing.T) {
	gs := &gameserverv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "mc", Namespace: testNSMinecraft},
		Spec:       gameserverv1alpha1.GameServerSpec{TemplateRef: gameserverv1alpha1.TemplateRef{Name: "t"}},
	}
	tpl := &gameserverv1alpha1.GameTemplate{
		Spec: gameserverv1alpha1.GameTemplateSpec{
			Storage: gameserverv1alpha1.TemplateStorage{DefaultSize: "5Gi"},
		},
	}
	pvc, err := BuildPVC(gs, tpl, nil)
	if err != nil {
		t.Fatalf("BuildPVC err: %v", err)
	}
	if pvc.Spec.DataSourceRef != nil {
		t.Errorf("expected nil DataSourceRef when snapshot is nil, got %+v", pvc.Spec.DataSourceRef)
	}
}

func TestLoadLocation(t *testing.T) {
	if loc, err := loadLocation(""); err != nil || loc.String() != "UTC" {
		t.Errorf("empty tz should be UTC, got %q err=%v", loc, err)
	}
	if _, err := loadLocation("Europe/Zurich"); err != nil {
		t.Errorf("Europe/Zurich should load, got %v", err)
	}
	if _, err := loadLocation("Not/A/Zone"); err == nil {
		t.Error("bogus tz should error")
	}
}

func TestSchedulerParse(t *testing.T) {
	if _, err := scheduleParser.Parse("0 4 * * *"); err != nil {
		t.Errorf("standard cron failed to parse: %v", err)
	}
	if _, err := scheduleParser.Parse("garbage"); err == nil {
		t.Error("garbage cron should fail to parse")
	}
}
