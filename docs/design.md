# Design

## Why not Pterodactyl / Pelican / Wings

Pterodactyl and Pelican expose game servers via a "Panel + Wings" split.
Wings is a Go daemon that owns the host's Docker socket, per-server local
disk, and SFTP; it is not a Kubernetes pod. Every attempt to run it inside
Kubernetes (kubectyl, java-wings) has been either abandoned or a toy.

This project takes the opposite tack: **game servers are first-class
Kubernetes objects**. No Panel-protocol compat layer. Configuration flows
through CRDs and GitOps; there is no web UI.

## Two CRDs

### `GameTemplate` (cluster-scoped)

A reusable class definition. Encodes an image, ports, env, config-key
schema, storage layout, security profile, and update strategy. Analogous
to a Pterodactyl "Egg" but simple key→env mapping — no shell scripts,
because it's meant to describe *the image*, not a compatibility layer for
one.

### `GameServer` (namespaced)

A single running instance. References a `GameTemplate` and supplies
per-instance `.spec.config`, resources, node placement, storage overrides,
and Service exposure. Owns exactly one Deployment (replicas=1), one PVC,
and one Service per exposure type. `spec.suspend: true` scales to 0
without losing the world.

## Reconciliation

`GameServerReconciler` is the workhorse:

1. Fetch the referenced `GameTemplate`. Set `TemplateResolved` condition.
2. Validate `.spec.config` against the template's `configKeys`
   (types: string / int / bool / enum; required + default; unknown keys
   are rejected). Set `ConfigValid` condition.
3. Ensure the profile's ServiceAccount exists in-namespace (skipped for
   `anyuid`; that SA is admin-managed).
4. Apply owned PVC, Deployment, Service(s) with
   `controllerutil.SetControllerReference` for GC.
5. Roll status forward: `.status.phase` (`Pending | Provisioning | Ready
   | Degraded | Stopped`), `.status.address`, condition list.

`GameTemplateReconciler` is lightweight: it maintains a
`.status.serversRegistered` count and enqueues fresh reconciles when a
`GameServer` referencing it changes. Cascade of template edits to running
servers is handled by the *GameServer* reconciler, which also watches
`GameTemplate`.

## OpenShift `restricted-v2` posture

Default emitted pod spec:

- `runAsNonRoot: true`, `seccompProfile: RuntimeDefault`
- container: `allowPrivilegeEscalation: false`, `capabilities.drop: ["ALL"]`
- `runAsUser` / `fsGroup` **unset** — OpenShift injects them from
  namespace annotations. On plain K8s the pod admits with defaults.
- No `hostPath`, `hostNetwork`, `hostPID`, `hostIPC`, privileged. Ever.

The manager itself runs read-only-root, drop-all-caps, non-root — same
posture, tighter.

`anyuid` is an explicit opt-in per `GameTemplate.spec.securityProfile`.
The operator emits `serviceAccountName: gameserver-anyuid` for those
pods; cluster-admin binds that SA to the `anyuid` SCC out of band. The
operator never grants SCCs. See [openshift.md](openshift.md).

## Reuse

`internal/controller/security.go` codifies the securityContext pattern
that also lives in kubectyl/kuber's archived `securitycontext.go` — that
pattern is exactly `restricted-v2`-shaped and worth reusing. The rest of
kubectyl (raw-Pod-only, `hostPath` mounts, Wings HTTP protocol) is
explicitly not reused; those choices are what we set out to avoid.

## Roadmap

- **Phase 2** — admission webhook for `configSchema` validation; onboard
  a second game (Valheim) to prove the abstraction.
- **Phase 3** — `Backup` and `BackupSchedule` CRDs wrapping
  `VolumeSnapshot` (works with any CSI driver that supports snapshots).
- **Phase 4** — Prometheus metrics, per-game protocol probes, structured
  events.
- **Phase 5** — OLM bundle for one-click install on OpenShift; Helm chart
  for plain K8s.
- **Phase 6 (stretch)** — console subresource, file management sidecar.
  Only after these does a UI become viable.
