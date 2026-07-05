# OpenShift Posture

## SCC by default

Every game pod the operator creates satisfies OpenShift's default
`restricted-v2` SCC:

- `runAsNonRoot: true`, `seccompProfile.type: RuntimeDefault`
- `allowPrivilegeEscalation: false`, `capabilities.drop: ["ALL"]`
- `runAsUser` / `fsGroup` are **unset** on purpose so the namespace's UID
  range annotations (`openshift.io/sa.scc.uid-range`,
  `openshift.io/sa.scc.supplemental-groups`) populate them
- no `hostPath`, `hostNetwork`, `hostPID`, `hostIPC`, privileged

The manager pod itself runs read-only-root, non-root, drop-all-caps.

Install:

```sh
oc new-project gameserver-system
oc apply -k config/openshift
```

Nothing further is needed for `securityProfile: restricted` (the default).
The operator creates a per-namespace `gameserver-restricted` ServiceAccount
in each namespace that hosts a `GameServer`; the default SCC admits it.

## `anyuid` escape hatch

Some game images (`itzg/minecraft-server` under some configurations, some
older jar-based servers) do not handle running as an arbitrary UID and
either refuse or crash their entrypoint. For these, set
`spec.securityProfile: anyuid` on the `GameTemplate`.

That opts the pod out of `runAsNonRoot`. The pod runs under
`serviceAccountName: gameserver-anyuid`, which cluster-admin must have
bound to OpenShift's `anyuid` SCC. The operator never grants SCCs on its
own — that stays admin-gated.

Install the ClusterRole once per cluster:

```sh
oc apply -f config/openshift/anyuid-scc-clusterrole.yaml
```

Per namespace that hosts an anyuid-profile GameServer:

```sh
oc create sa gameserver-anyuid -n <ns>
# edit the sample RoleBinding for the target namespace, then apply:
oc apply -f config/openshift/anyuid-rolebinding.example.yaml -n <ns>
```

## Verifying the assigned SCC

After a game pod becomes Ready, confirm the SCC OpenShift admitted it under:

```sh
oc get pod <pod> -o jsonpath='{.metadata.annotations.openshift\.io/scc}'
```

For `restricted` profile you should see `restricted-v2`. For `anyuid`
profile you should see `anyuid`. Anything else — and especially anything
requiring `privileged` — is a bug; open an issue.

## Namespace labels

Recommended labels on the namespace hosting `GameServer` objects:

```yaml
metadata:
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/enforce-version: latest
```

For `anyuid`-profile GameServers, downgrade `enforce` to `baseline` (or
use a separate namespace). Do NOT run privileged workloads next to game
servers.

## What's not solved

- **NodePort range.** Templates that expose games via NodePort must fall
  in the cluster's allowed port range (default `30000-32767`). Firewall
  rules upstream of the cluster still need to forward the physical port
  if the game should be reachable from outside the LAN.
- **PVC snapshots.** MVP does not schedule backups. Phase 3 adds a
  `Backup` CRD wrapping `VolumeSnapshot` (Synology CSI supports these).
- **External DNS.** `.status.address` reports the resolved connect
  string; hooking it into ExternalDNS or a Cloudflare tunnel is a caller
  concern for now.
