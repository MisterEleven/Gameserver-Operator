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

func mkTemplate(keys []gameserverv1alpha1.ConfigKey) *gameserverv1alpha1.GameTemplate {
	return &gameserverv1alpha1.GameTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tpl"},
		Spec: gameserverv1alpha1.GameTemplateSpec{
			Image:      "example/game:1",
			ConfigKeys: keys,
		},
	}
}

func mkServer(config map[string]string) *gameserverv1alpha1.GameServer {
	return &gameserverv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "gs", Namespace: "ns"},
		Spec: gameserverv1alpha1.GameServerSpec{
			TemplateRef: gameserverv1alpha1.TemplateRef{Name: "tpl"},
			Config:      config,
		},
	}
}

func TestValidateConfig_UnknownKey(t *testing.T) {
	tpl := mkTemplate([]gameserverv1alpha1.ConfigKey{{Name: "MOTD", EnvVar: "MOTD"}})
	gs := mkServer(map[string]string{"NOPE": "x"})
	if err := ValidateConfig(gs, tpl); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestValidateConfig_IntType(t *testing.T) {
	tpl := mkTemplate([]gameserverv1alpha1.ConfigKey{{Name: "MEM", EnvVar: "MEMORY", Type: "int"}})
	if err := ValidateConfig(mkServer(map[string]string{"MEM": "2048"}), tpl); err != nil {
		t.Fatalf("valid int rejected: %v", err)
	}
	if err := ValidateConfig(mkServer(map[string]string{"MEM": "abc"}), tpl); err == nil {
		t.Fatal("invalid int accepted")
	}
}

func TestValidateConfig_EnumType(t *testing.T) {
	tpl := mkTemplate([]gameserverv1alpha1.ConfigKey{{
		Name: "MODE", EnvVar: "MODE", Type: "enum", Enum: []string{"survival", "creative"},
	}})
	if err := ValidateConfig(mkServer(map[string]string{"MODE": "creative"}), tpl); err != nil {
		t.Fatalf("valid enum rejected: %v", err)
	}
	if err := ValidateConfig(mkServer(map[string]string{"MODE": "hardcore"}), tpl); err == nil {
		t.Fatal("invalid enum accepted")
	}
}

func TestValidateConfig_RequiredKey(t *testing.T) {
	tpl := mkTemplate([]gameserverv1alpha1.ConfigKey{{
		Name: "EULA", EnvVar: "EULA", Required: true,
	}})
	if err := ValidateConfig(mkServer(nil), tpl); err == nil {
		t.Fatal("missing required key accepted")
	}
	if err := ValidateConfig(mkServer(map[string]string{"EULA": "true"}), tpl); err != nil {
		t.Fatalf("required key with value rejected: %v", err)
	}
}

func TestValidateConfig_RequiredWithDefault(t *testing.T) {
	tpl := mkTemplate([]gameserverv1alpha1.ConfigKey{{
		Name: "EULA", EnvVar: "EULA", Required: true, Default: "true",
	}})
	if err := ValidateConfig(mkServer(nil), tpl); err != nil {
		t.Fatalf("required key with default should pass: %v", err)
	}
}

func TestResolveEnv_ConfigDefaultsAndOverrides(t *testing.T) {
	tpl := mkTemplate([]gameserverv1alpha1.ConfigKey{
		{Name: "EULA", EnvVar: "EULA", Default: "true"},
		{Name: "MEMORY", EnvVar: "MEMORY", Default: "1G"},
	})
	gs := mkServer(map[string]string{"MEMORY": "4G"})
	env := resolveEnv(gs, tpl)
	got := map[string]string{}
	for _, e := range env {
		got[e.Name] = e.Value
	}
	if got["EULA"] != "true" {
		t.Errorf("EULA default not applied: %q", got["EULA"])
	}
	if got["MEMORY"] != "4G" {
		t.Errorf("MEMORY override not applied: %q", got["MEMORY"])
	}
}

func TestBuildServices_GroupsByExposure(t *testing.T) {
	tpl := &gameserverv1alpha1.GameTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tpl"},
		Spec: gameserverv1alpha1.GameTemplateSpec{
			Image: "x",
			Ports: []gameserverv1alpha1.TemplatePort{
				{Name: "game", ContainerPort: 25565, ExposeAs: gameserverv1alpha1.ExposureNodePort, Primary: true},
				{Name: "rcon", ContainerPort: 25575, ExposeAs: gameserverv1alpha1.ExposureClusterIP},
			},
		},
	}
	gs := mkServer(nil)
	svcs := BuildServices(gs, tpl)
	if len(svcs) != 2 {
		t.Fatalf("expected 2 services, got %d", len(svcs))
	}
}
