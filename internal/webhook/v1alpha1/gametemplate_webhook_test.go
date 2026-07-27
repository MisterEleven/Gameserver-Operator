/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

func tplWith(mutate func(*gameserverv1alpha1.GameTemplate)) *gameserverv1alpha1.GameTemplate {
	tpl := &gameserverv1alpha1.GameTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: gameserverv1alpha1.GameTemplateSpec{
			Image: "example/game:1",
			Ports: []gameserverv1alpha1.TemplatePort{
				{Name: testPortNameGame, ContainerPort: 25565, Primary: true},
			},
			ConfigKeys: []gameserverv1alpha1.ConfigKey{
				{Name: testKeyMOTD, EnvVar: testEnvMOTD},
			},
		},
	}
	if mutate != nil {
		mutate(tpl)
	}
	return tpl
}

func TestTemplateWebhook_ValidTemplateAdmitted(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	if _, err := v.ValidateCreate(context.Background(), tplWith(nil)); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
}

func TestTemplateWebhook_DuplicatePortName(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	bad := tplWith(func(t *gameserverv1alpha1.GameTemplate) {
		t.Spec.Ports = append(t.Spec.Ports, gameserverv1alpha1.TemplatePort{
			Name: testPortNameGame, ContainerPort: 25575,
		})
	})
	_, err := v.ValidateCreate(context.Background(), bad)
	if err == nil {
		t.Fatal("expected rejection for duplicate port name")
	}
	if !strings.Contains(err.Error(), "game") {
		t.Errorf("error should reference the duplicated name, got: %v", err)
	}
}

func TestTemplateWebhook_DuplicateContainerPort(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	bad := tplWith(func(t *gameserverv1alpha1.GameTemplate) {
		t.Spec.Ports = append(t.Spec.Ports, gameserverv1alpha1.TemplatePort{
			Name: "rcon", ContainerPort: 25565,
		})
	})
	if _, err := v.ValidateCreate(context.Background(), bad); err == nil {
		t.Fatal("expected rejection for duplicate containerPort")
	}
}

func TestTemplateWebhook_MultiplePrimaries(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	bad := tplWith(func(t *gameserverv1alpha1.GameTemplate) {
		t.Spec.Ports = append(t.Spec.Ports, gameserverv1alpha1.TemplatePort{
			Name: "rcon", ContainerPort: 25575, Primary: true,
		})
	})
	_, err := v.ValidateCreate(context.Background(), bad)
	if err == nil {
		t.Fatal("expected rejection for two primary ports")
	}
	if !strings.Contains(err.Error(), "primary") {
		t.Errorf("error should mention primary, got: %v", err)
	}
}

func TestTemplateWebhook_DuplicateConfigKey(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	bad := tplWith(func(t *gameserverv1alpha1.GameTemplate) {
		t.Spec.ConfigKeys = append(t.Spec.ConfigKeys, gameserverv1alpha1.ConfigKey{
			Name: testKeyMOTD, EnvVar: "MOTD2",
		})
	})
	if _, err := v.ValidateCreate(context.Background(), bad); err == nil {
		t.Fatal("expected rejection for duplicate config key name")
	}
}

func TestTemplateWebhook_DuplicateEnvVar(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	bad := tplWith(func(t *gameserverv1alpha1.GameTemplate) {
		t.Spec.ConfigKeys = append(t.Spec.ConfigKeys, gameserverv1alpha1.ConfigKey{
			Name: "motd2", EnvVar: testEnvMOTD,
		})
	})
	if _, err := v.ValidateCreate(context.Background(), bad); err == nil {
		t.Fatal("expected rejection for duplicate envVar")
	}
}

func TestTemplateWebhook_EnumWithoutValuesRejected(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	bad := tplWith(func(t *gameserverv1alpha1.GameTemplate) {
		t.Spec.ConfigKeys = []gameserverv1alpha1.ConfigKey{
			{Name: "mode", EnvVar: "MODE", Type: testConfigKeyEnum},
		}
	})
	_, err := v.ValidateCreate(context.Background(), bad)
	if err == nil {
		t.Fatal("expected rejection for enum type with no values")
	}
	if !strings.Contains(err.Error(), "enum") {
		t.Errorf("error should mention enum, got: %v", err)
	}
}

func TestTemplateWebhook_EnumOnNonEnumTypeRejected(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	bad := tplWith(func(t *gameserverv1alpha1.GameTemplate) {
		t.Spec.ConfigKeys = []gameserverv1alpha1.ConfigKey{
			{Name: "k", EnvVar: "K", Type: "string", Enum: []string{"a"}},
		}
	})
	if _, err := v.ValidateCreate(context.Background(), bad); err == nil {
		t.Fatal("expected rejection for enum values on non-enum type")
	}
}

func TestTemplateWebhook_DefaultNotInEnum(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	bad := tplWith(func(t *gameserverv1alpha1.GameTemplate) {
		t.Spec.ConfigKeys = []gameserverv1alpha1.ConfigKey{
			{Name: "mode", EnvVar: "MODE", Type: testConfigKeyEnum, Enum: []string{"easy", "hard"}, Default: "impossible"},
		}
	})
	_, err := v.ValidateCreate(context.Background(), bad)
	if err == nil {
		t.Fatal("expected rejection for default outside the enum")
	}
	if !strings.Contains(err.Error(), "impossible") {
		t.Errorf("error should quote the bad default, got: %v", err)
	}
}

func TestTemplateWebhook_UpdateReValidates(t *testing.T) {
	v := &GameTemplateCustomValidator{}
	old := tplWith(nil)
	newTpl := tplWith(func(t *gameserverv1alpha1.GameTemplate) {
		t.Spec.Ports = append(t.Spec.Ports, gameserverv1alpha1.TemplatePort{
			Name: testPortNameGame, ContainerPort: 25575, // duplicate name
		})
	})
	if _, err := v.ValidateUpdate(context.Background(), old, newTpl); err == nil {
		t.Fatal("expected update to be rejected when new template is invalid")
	}
}
