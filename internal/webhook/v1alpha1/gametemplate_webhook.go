/*
Copyright 2026 Timo Feddern.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
)

var gametemplatelog = logf.Log.WithName("gametemplate-webhook")

// SetupGameTemplateWebhookWithManager registers the validating webhook for
// GameTemplate. Templates are self-contained (cluster-scoped, no cross-
// references), so no client is needed on the validator.
func SetupGameTemplateWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &gameserverv1alpha1.GameTemplate{}).
		WithValidator(&GameTemplateCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-gameserver-feddern-dev-v1alpha1-gametemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=gameserver.feddern.dev,resources=gametemplates,verbs=create;update,versions=v1alpha1,name=vgametemplate-v1alpha1.kb.io,admissionReviewVersions=v1

// GameTemplateCustomValidator catches template authoring mistakes at admit
// time: duplicate port/config-key names, empty enum lists, etc.
type GameTemplateCustomValidator struct{}

func (v *GameTemplateCustomValidator) ValidateCreate(_ context.Context, tpl *gameserverv1alpha1.GameTemplate) (admission.Warnings, error) {
	gametemplatelog.V(1).Info("validating GameTemplate create", "name", tpl.GetName())
	return nil, validateTemplate(tpl)
}

func (v *GameTemplateCustomValidator) ValidateUpdate(_ context.Context, _, newTpl *gameserverv1alpha1.GameTemplate) (admission.Warnings, error) {
	gametemplatelog.V(1).Info("validating GameTemplate update", "name", newTpl.GetName())
	return nil, validateTemplate(newTpl)
}

func (v *GameTemplateCustomValidator) ValidateDelete(_ context.Context, _ *gameserverv1alpha1.GameTemplate) (admission.Warnings, error) {
	return nil, nil
}

// validateTemplate walks the spec looking for authoring mistakes the CRD
// schema can't catch on its own. Returns nil on success or a
// Status(Invalid) error listing every field problem found.
func validateTemplate(tpl *gameserverv1alpha1.GameTemplate) error {
	errs := field.ErrorList{}

	// Ports: names and containerPorts must both be unique. CRD schema
	// already enforces MinItems=1 and required.name / required.containerPort.
	portsPath := field.NewPath("spec", "ports")
	seenPortName := map[string]int{}
	seenContainerPort := map[int32]int{}
	for i, p := range tpl.Spec.Ports {
		pp := portsPath.Index(i)
		if prev, dup := seenPortName[p.Name]; dup {
			errs = append(errs, field.Duplicate(
				pp.Child("name"),
				fmt.Sprintf("%q also used at index %d", p.Name, prev),
			))
		} else {
			seenPortName[p.Name] = i
		}
		if prev, dup := seenContainerPort[p.ContainerPort]; dup {
			errs = append(errs, field.Duplicate(
				pp.Child("containerPort"),
				fmt.Sprintf("%d also used at index %d", p.ContainerPort, prev),
			))
		} else {
			seenContainerPort[p.ContainerPort] = i
		}
	}
	primaries := 0
	for _, p := range tpl.Spec.Ports {
		if p.Primary {
			primaries++
		}
	}
	if primaries > 1 {
		errs = append(errs, field.Forbidden(
			portsPath,
			"at most one port may be marked primary",
		))
	}

	// ConfigKeys: names and envVars must be unique; enum type requires a
	// non-empty enum list; default value (if any) must satisfy the type.
	keysPath := field.NewPath("spec", "configKeys")
	seenKeyName := map[string]int{}
	seenEnvVar := map[string]int{}
	for i, k := range tpl.Spec.ConfigKeys {
		kp := keysPath.Index(i)
		if prev, dup := seenKeyName[k.Name]; dup {
			errs = append(errs, field.Duplicate(
				kp.Child("name"),
				fmt.Sprintf("%q also used at index %d", k.Name, prev),
			))
		} else {
			seenKeyName[k.Name] = i
		}
		if prev, dup := seenEnvVar[k.EnvVar]; dup {
			errs = append(errs, field.Duplicate(
				kp.Child("envVar"),
				fmt.Sprintf("%q also used at index %d", k.EnvVar, prev),
			))
		} else {
			seenEnvVar[k.EnvVar] = i
		}
		if k.Type == gameserverv1alpha1.ConfigKeyTypeEnum && len(k.Enum) == 0 {
			errs = append(errs, field.Required(
				kp.Child("enum"),
				"enum type requires a non-empty enum list",
			))
		}
		if k.Type != gameserverv1alpha1.ConfigKeyTypeEnum && len(k.Enum) > 0 {
			errs = append(errs, field.Forbidden(
				kp.Child("enum"),
				fmt.Sprintf("enum values only apply when type=enum (got type=%q)", k.Type),
			))
		}
		if k.Default != "" && k.Type == gameserverv1alpha1.ConfigKeyTypeEnum && !slices.Contains(k.Enum, k.Default) {
			errs = append(errs, field.Invalid(
				kp.Child("default"),
				k.Default,
				fmt.Sprintf("default %q is not in enum list %v", k.Default, k.Enum),
			))
		}
	}

	if len(errs) == 0 {
		return nil
	}
	gvk := schema.GroupKind{Group: gameserverv1alpha1.SchemeGroupVersion.Group, Kind: "GameTemplate"}
	return apierrors.NewInvalid(gvk, tpl.GetName(), errs)
}
