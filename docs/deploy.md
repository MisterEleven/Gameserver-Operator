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

## Publishing the operator image

`.github/workflows/release.yml` builds and pushes to GHCR automatically:

- Push to `main`              → `latest`, `main`, `sha-<short>`
- Push git tag `vX.Y.Z`       → image tags `X.Y.Z`, `X.Y`, `X`, `latest`
- Pull request                → build only (Dockerfile smoke test)

Note the `v` prefix is stripped in the image tag — the git convention is
`vX.Y.Z`, the OCI convention is `X.Y.Z`. `docker/metadata-action` does the
translation for you.

`config/manager/kustomization.yaml` pins the tag ArgoCD consumes
(currently `0.1.0`). Roll forward by tagging a new release, then
bumping the tag in kustomize in a follow-up commit.

### Bootstrapping — the first release

The image `0.1.0` won't exist until you create the tag. Order matters:

```sh
cd ~/Developer/PRIVAT/gameserver
git push -u origin main               # kicks off CI, builds "latest" + "main"
git tag v0.1.0
git push --tags                        # kicks off CI, builds 0.1.0
```

Once the workflow's green under Actions → Release, the `0.1.0` image tag
is in GHCR and ArgoCD can pull it.

### GHCR package visibility

GHCR packages default to private. Either:

1. Public (simplest): GitHub → your `gameserver-operator` package page →
   Package settings → Change visibility → Public.
2. Private: add an `imagePullSecret` to the manager Deployment via a
   kustomize patch. Not covered here.

### Local one-off build (only if CI isn't available yet)

```sh
echo $GH_PAT | docker login ghcr.io -u MisterEleven --password-stdin
docker buildx create --use --name gsv-builder 2>/dev/null || true
docker buildx build --platform linux/amd64 \
    --tag ghcr.io/mistereleven/gameserver-operator:0.1.0 \
    --push .
```

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

```sh
# 1. make changes, commit, push main → CI builds :latest, :main, :sha-<>
git push

# 2. cut a release
git tag v0.1.1
git push --tags                        # CI builds :0.1.1, :0.1, :0

# 3. point the deploy at it
# edit config/manager/kustomization.yaml — newTag: "0.1.1"
git commit -am "chore: bump manager image to 0.1.1"
git push                                # ArgoCD notices and rolls
```

ArgoCD auto-heals on the operator Application (selfHeal is on), so the
manager Deployment rolls to the new image within a minute of the
kustomize commit. The Minecraft server pod is untouched by an operator
upgrade — its Deployment only re-rolls when the reconciler decides to
(a template edit or a `GameServer` spec edit).

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
