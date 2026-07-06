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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

// buildScheme returns a scheme that knows about our v1alpha1 types.
func buildScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := gameserverv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("scheme setup: %v", err)
	}
	return s
}

func minimalTemplate(name string) *gameserverv1alpha1.GameTemplate {
	return &gameserverv1alpha1.GameTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: gameserverv1alpha1.GameTemplateSpec{
			Image: "example/game:1",
			Ports: []gameserverv1alpha1.TemplatePort{{
				Name:          "game",
				ContainerPort: 12345,
			}},
			ConfigKeys: []gameserverv1alpha1.ConfigKey{
				{Name: "motd", EnvVar: "MOTD", Default: "hi"},
				{Name: "difficulty", EnvVar: "DIFFICULTY", Type: "enum", Enum: []string{"easy", "hard"}, Default: "easy"},
			},
		},
	}
}

func gsFor(templateName string, config map[string]string) *gameserverv1alpha1.GameServer {
	return &gameserverv1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "ns"},
		Spec: gameserverv1alpha1.GameServerSpec{
			TemplateRef: gameserverv1alpha1.TemplateRef{Name: templateName},
			Config:      config,
		},
	}
}

func TestGameServerWebhook_Accept(t *testing.T) {
	scheme := buildScheme(t)
	tpl := minimalTemplate("mc")
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tpl).Build()
	v := &GameServerCustomValidator{Client: cli}

	warnings, err := v.ValidateCreate(context.Background(), gsFor("mc", map[string]string{
		"motd":       "hello",
		"difficulty": "hard",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestGameServerWebhook_UnknownKeyRejected(t *testing.T) {
	scheme := buildScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(minimalTemplate("mc")).Build()
	v := &GameServerCustomValidator{Client: cli}

	_, err := v.ValidateCreate(context.Background(), gsFor("mc", map[string]string{"nope": "x"}))
	if err == nil {
		t.Fatal("expected rejection for unknown config key")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should name the bad key, got: %v", err)
	}
}

func TestGameServerWebhook_EnumRejected(t *testing.T) {
	scheme := buildScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(minimalTemplate("mc")).Build()
	v := &GameServerCustomValidator{Client: cli}

	_, err := v.ValidateCreate(context.Background(), gsFor("mc", map[string]string{"difficulty": "impossible"}))
	if err == nil {
		t.Fatal("expected rejection for out-of-enum value")
	}
}

func TestGameServerWebhook_MissingTemplateWarns(t *testing.T) {
	scheme := buildScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	v := &GameServerCustomValidator{Client: cli}

	warnings, err := v.ValidateCreate(context.Background(), gsFor("nope", nil))
	if err != nil {
		t.Fatalf("missing template should warn, not error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning about the missing template")
	}
}

func TestGameServerWebhook_EmptyTemplateRefRejected(t *testing.T) {
	scheme := buildScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	v := &GameServerCustomValidator{Client: cli}

	_, err := v.ValidateCreate(context.Background(), gsFor("", nil))
	if err == nil {
		t.Fatal("expected rejection for empty templateRef.name")
	}
}

func TestGameServerWebhook_UpdateReValidates(t *testing.T) {
	scheme := buildScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(minimalTemplate("mc")).Build()
	v := &GameServerCustomValidator{Client: cli}

	old := gsFor("mc", map[string]string{"motd": "before"})
	newGS := gsFor("mc", map[string]string{"unknown": "after"})
	_, err := v.ValidateUpdate(context.Background(), old, newGS)
	if err == nil {
		t.Fatal("expected update to be rejected when new config is invalid")
	}
}

func TestGameServerWebhook_DeleteIsNoop(t *testing.T) {
	v := &GameServerCustomValidator{}
	if _, err := v.ValidateDelete(context.Background(), gsFor("mc", nil)); err != nil {
		t.Fatalf("delete should not error: %v", err)
	}
}
