# Onboarding a new game

Adding a new game is a pure-YAML exercise most of the time. Write a
`GameTemplate` describing the container image, ports, and configurable
knobs; users then create `GameServer` objects that reference it.

Controller code only needs a change if the game requires a probe protocol
we don't handle yet (Minecraft's SLP over TCP, Steam A2S over UDP, etc.).

## Checklist

Given an OCI image for a game, you need:

1. **Image + tag.** Prefer well-maintained community images
   (`itzg/minecraft-server`, `lloesche/valheim-server`,
   `thijsvanloef/palworld-server-docker`, `factoriotools/factorio`).
2. **Ports.** List every port the process listens on. Mark one `primary`
   — that becomes `.status.address`. Set `exposeAs` per port:
   - `ClusterIP` for admin/rcon/query ports the world doesn't need.
   - `NodePort` for the game port when it should be reachable from LAN.
   - `LoadBalancer` if you have MetalLB / a cloud provider.
3. **Env vars the image cares about.** Two flavors:
   - Static (always the same): put in `spec.env`.
   - Per-instance (users set differently per `GameServer`): declare a
     `configKey`. Every `configKey` needs a `name` (what the GameServer
     sets), an `envVar` (what the image reads), and optionally
     `type` (`string|int|bool|enum`), `enum`, `default`, `required`.
4. **Persistent data dir.** Whatever path the image writes worlds/saves
   to (Minecraft: `/data`, Valheim: `/config`, Factorio: `/factorio`).
   Set `spec.storage.dataPath` accordingly. `defaultSize` should be a
   reasonable starting PVC size; users override per-instance.
5. **Security profile.** Try `restricted` first. If the pod refuses to
   start under OpenShift restricted-v2 or logs permission-denied on the
   data volume, drop to `anyuid` and document why in the template.
6. **Update strategy.** Leave `Recreate` unless the game supports
   graceful hot-swap. Most game servers require exclusive file locks on
   the world dir, so `Recreate` is right.
7. **Probes.** MVP defaults to a TCP probe on the primary port. If the
   game has a lightweight health endpoint, set `spec.probes.readiness`
   explicitly.

## Example: Valheim (untested — treat as a scaffold)

```yaml
apiVersion: gameserver.feddern.dev/v1alpha1
kind: GameTemplate
metadata:
  name: valheim
spec:
  image: lloesche/valheim-server:latest
  securityProfile: restricted
  updateStrategy: Recreate
  ports:
    - name: game
      containerPort: 2456
      protocol: UDP
      exposeAs: NodePort
      primary: true
    - name: query
      containerPort: 2457
      protocol: UDP
      exposeAs: NodePort
  storage:
    dataPath: /config
    defaultSize: 5Gi
  configKeys:
    - name: server-name
      envVar: SERVER_NAME
      required: true
    - name: world-name
      envVar: WORLD_NAME
      required: true
    - name: password
      envVar: SERVER_PASS
      required: true
      description: min 5 chars, cannot contain server name
```

Then to run one:

```yaml
apiVersion: gameserver.feddern.dev/v1alpha1
kind: GameServer
metadata:
  name: valheim-friends
spec:
  templateRef:
    name: valheim
  config:
    server-name: "my-valheim-server"
    world-name: "midgard"
    password: "sup3rsecret"
  storage:
    size: 20Gi
```

## When code changes ARE needed

- **Custom probe protocol.** If TCP-connect isn't a valid liveness signal
  (some games listen but haven't accepted world load yet), extend
  `internal/controller/builders.go` to emit a game-specific probe.
- **Multi-container pods** (game + rcon-proxy, game + gluetun VPN).
  Requires CRD extension for a `sidecars` field and reconciler support.
- **Init-container Egg-style install scripts.** Not in scope for MVP —
  pin fully-baked images instead. If forced, extend the reconciler to
  emit an InitContainer per `installScript` block.

Before opening a code change, ask: *can the image be rebuilt / re-tagged
to avoid the need?* Every controller code path is a maintenance liability.
