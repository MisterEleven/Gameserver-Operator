# Deploying via ArgoCD (GitOps)

This is the runbook for taking the operator from a local kubebuilder tree to
running on the cluster via ArgoCD. Two git repos are involved:

- **this repo** (`gameserver-operator`) → pushed to
  `github.com/MisterEleven/gameserver-operator`. ArgoCD reads
  `config/openshift` from `HEAD` and reconciles it into
  the `gameserver-system` namespace.
- **the GitOps repo** (`LOTR-ARGOCD`) → holds the two
  `argoproj.io/v1alpha1` Application manifests
  (`apps/gameserver-operator.yaml`, `apps/minecraft-server.yaml`) and the
  first Minecraft workload (`minecraft-server/`).

## One-time: publish the operator image to GHCR

The manager pod's image is `ghcr.io/mistereleven/gameserver-operator:v0.1.0`,
set via `config/manager/kustomization.yaml`. Bump the tag whenever a new
image is pushed.

```sh
# Prereqs: docker (or podman with docker CLI shim), buildx, a GH PAT with
# write:packages scope, cwd == this repo.

echo $GH_PAT | docker login ghcr.io -u <github-user> --password-stdin

# The cluster is linux/amd64 (rivendell); dev boxes are often arm64.
# Use buildx to cross-build and push in one shot.
make docker-buildx \
    IMG=ghcr.io/mistereleven/gameserver-operator:v0.1.0 \
    PLATFORMS=linux/amd64
```

If `make docker-buildx` isn't wired for cross-arch by default, fall back:

```sh
docker buildx create --use --name gsv-builder || true
docker buildx build \
    --platform linux/amd64 \
    --tag ghcr.io/mistereleven/gameserver-operator:v0.1.0 \
    --push .
```

Make the package public in GitHub → your GHCR package page → "Package
settings" → "Change visibility" → Public. (Or keep it private and add an
`imagePullSecret` to the manager Deployment via a kustomize patch — out
of scope here.)

## Push the operator repo

```sh
# Create the repo on GitHub first (empty, no README/gitignore).
git remote add origin git@github.com:MisterEleven/gameserver-operator.git
git branch -M main
git push -u origin main
```

## Wire ArgoCD

Nothing to do inside the cluster imperatively — the LOTR-ARGOCD repo has
an app-of-apps that discovers new `apps/*.yaml` on its next reconcile.

```sh
cd ~/Developer/PRIVAT/LOTR-ARGOCD
git add apps/gameserver-operator.yaml apps/minecraft-server.yaml minecraft-server/
git commit -m "feat: gameserver-operator + minecraft-server"
git push
```

Within ~30s ArgoCD's `apps` Application will notice the new children and
create them:

```sh
export KUBECONFIG=~/Downloads/kubeconfig-lotr
oc -n openshift-gitops get applications | grep -E 'gameserver-operator|minecraft-server'
```

Force a refresh if you're impatient:

```sh
oc -n openshift-gitops patch application apps --type merge \
    -p '{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"hard"}}}'
```

## Watch it come up

```sh
# Operator manager pod
oc -n gameserver-system get pods -w

# CRDs registered?
oc get crd gameservers.gameserver.feddern.dev gametemplates.gameserver.feddern.dev

# Minecraft workload
oc -n minecraft get gameserver,pods,pvc,svc
oc -n minecraft get gameserver minecraft -o jsonpath='{.status.phase}{"\n"}'
```

`.status.phase` cycles `Pending → Provisioning → Ready`. First provision
is slow — the itzg image is ~700MB and Papermc downloads on first boot.

## Connect

```sh
oc -n minecraft get gameserver minecraft -o jsonpath='{.status.address}{"\n"}'
```

For a NodePort exposure the address prints `<node>:NNNNN`. Grab a node IP:

```sh
oc get nodes -o wide
```

Point a Minecraft Java client at `<node-ip>:NNNNN`.

## Iterating on the operator

Code change → new tag:

```sh
make docker-buildx IMG=ghcr.io/mistereleven/gameserver-operator:v0.1.1 PLATFORMS=linux/amd64
# Edit config/manager/kustomization.yaml — bump newTag to v0.1.1.
git commit -am "chore: v0.1.1"
git push
```

ArgoCD auto-heals on the operator Application (selfHeal is on), so the
manager Deployment will roll to the new image. The Minecraft server pod
is untouched by an operator upgrade — its Deployment only re-rolls when
the reconciler decides to (a template edit or a `GameServer` spec edit).

## Rolling back

```sh
oc -n openshift-gitops patch application gameserver-operator --type merge \
    -p '{"spec":{"source":{"targetRevision":"<previous-git-sha>"}}}'
```

Or edit the image tag in kustomize and commit.

## Troubleshooting

- **Pod stuck in `CreateContainerConfigError`** → check `oc describe pod`
  for image-pull failures. Most common: GHCR package still private.
- **Pod ready but game not reachable** → NodePort collisions or firewall.
  Try `oc port-forward svc/minecraft-nodeport 25565:25565 -n minecraft`
  as a quick loopback test.
- **`.status.phase=Degraded`** → look at `.status.conditions`; the
  reconciler writes the exact reason (`InvalidConfig`, `NotFound`, etc.).
- **Nothing happens after commit** → check the app-of-apps synced:
  `oc -n openshift-gitops get application apps -o jsonpath='{.status.sync.status}'`.
  Should be `Synced`.
