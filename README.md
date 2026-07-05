# gameserver-operator

Kubernetes-native operator for self-hosted game servers. Two CRDs —
`GameTemplate` (cluster-scoped, reusable per game) and `GameServer` (namespaced,
one running instance) — reconcile into standard Kubernetes objects (Deployment,
PVC, Service). Works on plain Kubernetes and on OpenShift under the
`restricted-v2` SCC by default; `anyuid` is an explicit opt-in per template.

Not compatible with the Pterodactyl/Pelican Wings protocol on purpose — see
`docs/design.md` for the rationale.

## Status

Alpha (`v1alpha1`). MVP scope: one `GameTemplate` for Minecraft ships in
`config/samples/`. Additional games are onboarded by writing more
`GameTemplate` YAML — no controller changes required. See
[docs/onboarding-a-game.md](docs/onboarding-a-game.md).

## Prerequisites

- Go 1.24+ (only needed to build from source)
- kubectl 1.28+
- A Kubernetes 1.28+ or OpenShift 4.16+ cluster
- (dev) kind 0.24+ for local smoke tests

## Quickstart — plain Kubernetes (kind)

```sh
kind create cluster
make install                   # apply CRDs
make deploy IMG=controller:latest
kubectl apply -f config/samples/gameserver_v1alpha1_gametemplate.yaml
kubectl apply -f config/samples/gameserver_v1alpha1_gameserver.yaml
kubectl get gameservers -w
```

When `.status.phase` is `Ready`, connect the Minecraft client to the
`.status.address`. If it's a `<node>:NNNNN` placeholder, resolve the node IP
manually — `kubectl get nodes -o wide` in a kind cluster shows the container
IP.

## Quickstart — OpenShift

```sh
oc new-project gameserver-system
oc apply -k config/openshift
# per game namespace using anyuid (only when you set securityProfile: anyuid):
oc create sa gameserver-anyuid -n <ns>
oc apply -f config/openshift/anyuid-rolebinding.example.yaml -n <ns>   # after editing namespace
```

Full posture and SCC details: [docs/openshift.md](docs/openshift.md).

## Design

- CRDs: [api/v1alpha1/gametemplate_types.go](api/v1alpha1/gametemplate_types.go),
  [api/v1alpha1/gameserver_types.go](api/v1alpha1/gameserver_types.go).
- Reconciler: [internal/controller/gameserver_controller.go](internal/controller/gameserver_controller.go).
- Pod / PVC / Service builders live in
  [internal/controller/builders.go](internal/controller/builders.go);
  security-context defaults in
  [internal/controller/security.go](internal/controller/security.go).
- Full design + roadmap: `docs/design.md`.

## Repo layout

```
api/v1alpha1/               CRD Go types
internal/controller/        reconcilers, builders, security helpers
cmd/main.go                 manager entrypoint
config/default/             kustomize base — plain Kubernetes
config/openshift/           kustomize overlay — SCC + anyuid RBAC
config/samples/             GameTemplate + GameServer for Minecraft
docs/                       design notes, OpenShift posture, onboarding guide
```

## Testing

```sh
make generate manifests    # regen CRDs after editing types
go test ./internal/controller -run 'TestValidate|TestResolve|TestBuild'   # pure unit tests
make test                  # envtest — spins apiserver+etcd
```

## License

Apache-2.0. See [LICENSE](LICENSE).
