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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	gameserverv1alpha1 "github.com/timofeddern/gameserver/api/v1alpha1"
	"github.com/timofeddern/gameserver/internal/controller"
)

var gameserverlog = logf.Log.WithName("gameserver-webhook")

// SetupGameServerWebhookWithManager registers the validating webhook. The
// validator holds a client so it can resolve the referenced GameTemplate
// (cluster-scoped) at admit time.
func SetupGameServerWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &gameserverv1alpha1.GameServer{}).
		WithValidator(&GameServerCustomValidator{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-gameserver-feddern-dev-v1alpha1-gameserver,mutating=false,failurePolicy=fail,sideEffects=None,groups=gameserver.feddern.dev,resources=gameservers,verbs=create;update,versions=v1alpha1,name=vgameserver-v1alpha1.kb.io,admissionReviewVersions=v1

// GameServerCustomValidator rejects GameServer objects whose .spec.config
// violates the referenced GameTemplate's ConfigKeys constraints.
type GameServerCustomValidator struct {
	Client client.Client
}

// ValidateCreate is called on `kubectl apply` / `oc apply` for new objects.
func (v *GameServerCustomValidator) ValidateCreate(ctx context.Context, gs *gameserverv1alpha1.GameServer) (admission.Warnings, error) {
	gameserverlog.V(1).Info("validating GameServer create", "name", gs.GetName(), "namespace", gs.GetNamespace())
	return v.validate(ctx, gs)
}

// ValidateUpdate re-runs the same checks; template refs and configs can both
// change between revisions.
func (v *GameServerCustomValidator) ValidateUpdate(ctx context.Context, _, newGS *gameserverv1alpha1.GameServer) (admission.Warnings, error) {
	gameserverlog.V(1).Info("validating GameServer update", "name", newGS.GetName(), "namespace", newGS.GetNamespace())
	return v.validate(ctx, newGS)
}

// ValidateDelete is a no-op — nothing about deletion is worth blocking.
func (v *GameServerCustomValidator) ValidateDelete(_ context.Context, _ *gameserverv1alpha1.GameServer) (admission.Warnings, error) {
	return nil, nil
}

// validate is shared between create and update. It fetches the referenced
// GameTemplate and defers to controller.ValidateConfig (same code the
// reconciler already uses).
func (v *GameServerCustomValidator) validate(ctx context.Context, gs *gameserverv1alpha1.GameServer) (admission.Warnings, error) {
	warnings := admission.Warnings{}
	fieldErrs := field.ErrorList{}

	if gs.Spec.TemplateRef.Name == "" {
		fieldErrs = append(fieldErrs, field.Required(
			field.NewPath("spec", "templateRef", "name"),
			"templateRef.name must be set",
		))
		return warnings, invalidError(gs, fieldErrs)
	}

	var tpl gameserverv1alpha1.GameTemplate
	if err := v.Client.Get(ctx, types.NamespacedName{Name: gs.Spec.TemplateRef.Name}, &tpl); err != nil {
		if apierrors.IsNotFound(err) {
			// Warn instead of reject — the reconciler already handles this
			// case gracefully and callers may legitimately apply the
			// GameServer before its template lands (rare, but supported).
			warnings = append(warnings, fmt.Sprintf(
				"referenced GameTemplate %q does not exist yet; the reconciler will wait for it",
				gs.Spec.TemplateRef.Name,
			))
			return warnings, nil
		}
		return warnings, fmt.Errorf("looking up GameTemplate %q: %w", gs.Spec.TemplateRef.Name, err)
	}

	if err := controller.ValidateConfig(gs, &tpl); err != nil {
		fieldErrs = append(fieldErrs, field.Invalid(
			field.NewPath("spec", "config"),
			gs.Spec.Config,
			err.Error(),
		))
	}

	if len(fieldErrs) > 0 {
		return warnings, invalidError(gs, fieldErrs)
	}
	return warnings, nil
}

// invalidError wraps a field.ErrorList as the sort of Status error the
// apiserver renders nicely to `kubectl apply`.
func invalidError(gs *gameserverv1alpha1.GameServer, errs field.ErrorList) error {
	gvk := schema.GroupKind{Group: gameserverv1alpha1.SchemeGroupVersion.Group, Kind: "GameServer"}
	return apierrors.NewInvalid(gvk, gs.GetName(), errs)
}
