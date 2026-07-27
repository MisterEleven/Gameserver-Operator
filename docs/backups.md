# Backups

Two CRDs cover the point-in-time snapshot lifecycle:

- **`Backup`** — one-shot snapshot of a `GameServer`'s data PVC.
- **`BackupSchedule`** — cron-driven, produces `Backup` objects and
  retains the newest `keep` of them.

Both ride on the Kubernetes-native `VolumeSnapshot` /
`VolumeSnapshotContent` / `VolumeSnapshotClass` API
(`snapshot.storage.k8s.io/v1`). Nothing in the operator names a CSI
driver — any driver that advertises snapshotting works (Synology CSI,
Longhorn, Ceph RBD, EBS, TopoLVM, Rook, Portworx, OpenEBS, …).

## One-shot backup

```yaml
apiVersion: gameserver.feddern.dev/v1alpha1
kind: Backup
metadata:
  name: mc-preupgrade
  namespace: minecraft
spec:
  gameServerRef:
    name: minecraft
  retainForever: true          # BackupSchedule GC skips it
```

Apply, then:

```sh
oc -n minecraft get backup -w
```

The Backup transitions **Pending → InProgress → Ready** as the child
`VolumeSnapshot` binds and reports `readyToUse: true`.
`.status.restoreSize` is populated when the CSI driver reports it.

`volumeSnapshotClassName` is optional: empty falls back to the cluster's
default `VolumeSnapshotClass` (marked
`snapshot.storage.kubernetes.io/is-default-class: "true"`). Set it
explicitly if the cluster has multiple classes.

## Nightly schedule

```yaml
apiVersion: gameserver.feddern.dev/v1alpha1
kind: BackupSchedule
metadata:
  name: minecraft-nightly
  namespace: minecraft
spec:
  gameServerRef:
    name: minecraft
  schedule: "0 4 * * *"        # 5-field cron; matches batch/v1 CronJob
  timeZone: Europe/Zurich
  keep: 7
```

Every fire creates a `Backup` named
`<schedule>-<yyyymmdd-hhmm>`. Retention is oldest-first: once more than
`keep` non-retainForever backups exist, the oldest are deleted. Deletion
cascades to the underlying `VolumeSnapshot` via owner-ref (and, with a
`Delete` deletionPolicy on the class, cleans up the CSI-side snapshot).

Suspend by setting `spec.suspend: true` — existing backups stay,
schedule doesn't fire again until unsuspended.

## Restoring into a new GameServer

`GameServer.spec.restoreFrom.backupName` seeds the new PVC from a
`Backup`'s VolumeSnapshot on first creation:

```yaml
apiVersion: gameserver.feddern.dev/v1alpha1
kind: GameServer
metadata:
  name: minecraft-clone
  namespace: minecraft
spec:
  templateRef: { name: minecraft-java }
  restoreFrom:
    backupName: mc-preupgrade
  config:
    type: FTBA
    ftb-modpack-id: "134"
    ftb-modpack-version-id: "100433"
    memory: "8G"
  storage: { size: 40Gi }
```

**Rules:**
- The referenced `Backup` must be in `phase: Ready`. Reconciler sets
  `RestoreSourceResolved=False` and phase `Degraded` otherwise.
- `restoreFrom` is honored **only when the PVC does not yet exist**. On
  subsequent reconciles it's inert (PVC `dataSourceRef` is immutable).
- To restore in place: delete the GameServer (cascades PVC teardown),
  then re-apply with `restoreFrom` set. The new PVC is provisioned from
  the snapshot.

## What the reconciler doesn't do (yet)

- **Quiescing** (`save-off` / `save-all flush` via rcon before the
  snapshot). Cold snapshot only; MC autosaves every ~45s so the worst
  case is a few minutes of chunk state, which the game recovers cleanly
  on next boot.
- **GFS retention** (daily/weekly/monthly buckets). Only `keep: N` by
  count.
- **Off-site push** (S3/restic). Snapshots live on the same storage
  backend as the source PVC; a NAS-side backup is the answer for that.

Any of the above is a reasonable next feature slice.
