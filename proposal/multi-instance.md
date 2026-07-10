# Multi-Instance Agent-Sandbox

## 1. Goal

Run agent-sandbox with `replicas > 1` behind a Service / LB without:

- per-user rate limits silently multiplying by N,
- background loops (scalers, pool syncer) doing N× work and stepping on each other,
- duplicate telemetry events for the same sandbox lifecycle.

This proposal lists the minimum changeset. Anything not mentioned is already safe (or out of scope, see §6).

## 2. What already works (no change)

| Surface | Why it's safe |
| --- | --- |
| HTTP API handlers | Stateless per request. Replica selection is up to the LB. |
| Sandbox router / proxy (`pkg/router`) | Looks up sandboxes by K8s API; no in-memory state. |
| Auth (token validation) | Stateless. |
| Runtime ConfigMap watcher | Each replica watches independently and applies to its own `config.Cfg`. |
| Informer caches | Each replica owns its own; K8s is the source of truth. |
| Telemetry emit (`pkg/telemetry`) | OTLP/HTTP to VictoriaLogs; many clients per backend is the normal case. |
| Capacity check (existing-sandbox count) | Reads via informer, cluster-wide truth. |
| Activator's last-request lookup | Backed by K8s Events, cluster-scoped. |

## 3. Risks and fixes

### 3.1 In-memory concurrency slot multiplies the limit by N

**Problem.** `pkg/capacity/limiter.go` keeps `UserLimiter.count` per process. With N replicas a user with `max_concurrency=3` can sustain `3×N` in-flight creates.

**Fix (chosen).** Replace the in-memory slot with an **informer-backed count of in-progress creates**:

- `CountByUserInCreate(user string) (int, error)` — count RSes labeled `sbx-user=<user>, sbx-pool=false` whose derived status is `Creating`.
- `AcquireCreate(user)` becomes: read both the existing capacity AND `CountByUserInCreate(user)`; if `inflight >= MaxConcurrency`, reject with 429.
- `Release` becomes a no-op (no in-memory counter to free).

Trade-off: slightly racy at the boundary — two replicas could see `inflight=2` simultaneously when the limit is 3, both admit a request, and the limit is briefly exceeded by 1. Acceptable for the use case.

**Rejected alternative.** Redis / etcd-backed counter. Adds a dep and a failure mode for what is fundamentally a soft limit.

### 3.2 Background loops and config bootstrap run on every replica

**Problem.** `main.go` unconditionally starts:

```go
cfg.CheckAndSaveConfigToConfigmap()
go scaler.NewScaler(...).RunScaling()
go pl.StartPoolSyncing()
```

With N replicas:

- **ConfigMap bootstrap race** — every replica tries to write the initial runtime ConfigMap. K8s rejects all but one via `AlreadyExists`, but the create attempts and conditional-update logic run N times.
- **Pool over-provisioning** — two replicas both see `pool=2, target=3` → both create → `pool=4`. Wasteful, not corrupting.
- **Duplicate delete / pause** — both replicas hit a timeout, both try to delete; K8s delete is idempotent (the second gets `NotFound`), but pause races on the RS's ResourceVersion (one wins, one fails with a `Conflict` log line).
- **Duplicate telemetry** — both emit `sandbox.delete` for the same RS → dashboard counts overcount.
- **Duplicate K8s Events** — `EventDelete` writes twice via the recorder.

**Fix (chosen).** K8s `Lease`-based leader election. Only the leader runs:

- `cfg.CheckAndSaveConfigToConfigmap()` — initial bootstrap.
- The scaler goroutine.
- The pool syncer goroutine.

The HTTP API, router, telemetry pipeline, informer caches, **and the ConfigMap watcher** keep running on every replica. The watcher is read-only, so non-leader replicas see config updates as soon as the leader writes them. Until the leader writes, non-leader replicas operate on their `envconfig`-loaded defaults — no deadlock.

Implementation outline:

```go
// pkg/leader/leader.go (new)
func RunAsLeader(ctx context.Context, client kubernetes.Interface, identity string,
                 onStart func(context.Context)) error {
    lock := &resourcelock.LeaseLock{
        LeaseMeta: metav1.ObjectMeta{Name: "agent-sandbox-leader", Namespace: ns},
        Client:    client.CoordinationV1(),
        LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
    }
    leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
        Lock: lock, LeaseDuration: 15 * time.Second,
        RenewDeadline: 10 * time.Second, RetryPeriod: 2 * time.Second,
        Callbacks: leaderelection.LeaderCallbacks{
            OnStartedLeading: onStart,
            OnStoppedLeading: func() { klog.Info("lost leadership") },
        },
    })
    return nil
}
```

`main.go` change:

```go
go leader.RunAsLeader(rootCtx, kubeClient, podName, func(ctx context.Context) {
    s := scaler.NewScaler(ctx, a, c, recorder)
    go s.RunScaling()
    go pl.StartPoolSyncing()
})
```

Identity is the pod name (`HOSTNAME` env). Lease lives in the same namespace as the sandboxes.

### 3.3 Why not redirect pool operations to the leader?

A natural follow-up: if the leader manages the pool syncer, should non-leader replicas redirect *create* requests (which acquire warm pool RSs) to the leader as well? That would eliminate the acquisition race entirely — only one replica writes to the pool.

**Decision: no.** The cost outweighs the benefit:

| Concern | With leader-only syncer (§3.2) | With request redirect added |
| --- | --- | --- |
| Pool replenishment | Single writer (leader). ✓ | Same. |
| Acquisition race | `AcquirePoolReplicaSet` already does `retry.OnError`. Loser sees a ResourceVersion conflict and picks a different warm RS. One extra K8s round-trip in the rare race case. | Eliminated. |
| Latency | Direct. | Extra in-cluster HTTP hop on every create. |
| Throughput ceiling | N×. | 1× (leader CPU / network). |
| Failure during leader handover | Create still works on the local replica. | 2–15s window where creates fail or hang. |
| Implementation | None beyond leader election. | Handler middleware that reads the Lease's `holderIdentity`, resolves it to a pod IP, reverse-proxies the request; needs leadership-change handling and loop guards. |

The race-and-retry path already converges in one or two iterations because pool size `N` means `N` candidates are available. If integration tests later show real contention (e.g. pool size 1 with many concurrent creates), revisit. The lower-cost escalation is to make `AcquirePoolReplicaSet` retry more aggressively, not to bottleneck through one replica.

### 3.4 Telemetry duplication on the create / pool-acquire path

**Problem.** Even with leader election covering scalers (§3.2), the create path runs on every replica that receives a `POST /api/v1/sandbox` — that's correct. But pool acquisition races (two replicas trying to claim the same warm RS) can each emit a `sandbox.create` event before one of them fails the ResourceVersion check.

**Fix (chosen).** No change needed at the emit layer. `Controller.Create` already emits exactly once per attempt — including the failed-acquire case — and that's the right semantic. The "two creates, one wins" pattern is **observable**, not a bug. Dashboard math (`create_total - create_failed = create_success`) stays accurate because the loser's event has `success=false`.

### 3.5 K8s Event recorder duplication

**Problem.** Solved by §3.2 — the scaler is the only path that emits `SandboxDelete` / `SandboxPaused` K8s events, and that's now leader-only.

## 4. Minimum changeset

In dependency order:

1. **`pkg/leader/leader.go`** (new) — Lease-based leader election helper, ~40 lines.
2. **`main.go`** — wrap `cfg.CheckAndSaveConfigToConfigmap()`, the scaler goroutine, and `pl.StartPoolSyncing()` in `leader.RunAsLeader(...)`. The ConfigMap watcher stays on every replica. Pass the pod name as identity (`os.Getenv("HOSTNAME")` fallback to `os.Hostname()`).
3. **`pkg/sandbox/controller.go`** — add `CountByUserInCreate(user) (int, error)`. Uses the existing informer; selector is `owner=agent-sandbox, sbx-user=<user>, sbx-pool=false`, then filter in-memory by `deriveSandboxStatus(rs) == Creating`.
4. **`pkg/capacity/limiter.go`** —
   - Drop `UserLimiter.count` field, `mu`, and `Release` (no-op).
   - `AcquireCreate` now does: `inflight, _ := controller.CountByUserInCreate(user)`; reject if `inflight >= MaxConcurrency`.
   - Callers in `pkg/handler/handlers.go` and `pkg/api/e2b/sandbox.go` keep the `if release != nil { defer release() }` shape — `release` is now always `nil`. (Or simplify the callers by dropping the `release` return value; small mechanical change.)
5. **RBAC** — the existing Role already grants `leases`. ✓ No manifest change required for leader election; confirm via `grep -i lease install.yaml`.

Estimated diff: ~150 lines across 4 files plus 1 new file.

## 5. Test plan

- **Unit.** Capacity limiter: with the controller mocked to return 0/1/2/3 in-flight, verify accept/reject behavior at `MaxConcurrency=3`.
- **Integration.** Start two `agent-sandbox` pods in a kind cluster.
  - Both pods alive, leader holds the Lease. Kill the leader pod → other pod takes over within `LeaseDuration` (15s).
  - Issue 6 concurrent create requests for one user with `max_concurrency=3` → exactly 3 succeed, 3 get 429 (slack of ±1 acceptable).
  - Time-out a sandbox → exactly one `sandbox.delete` telemetry record, exactly one K8s Event.
  - Pool template with `size=3` → pool count stabilizes at 3, never exceeds (`watch kubectl get rs`).
- **Dashboard.** Per-user create / delete counts match `kubectl get rs` history during the integration test.

## 6. Out of scope

- **Sharding** — every replica still has full visibility / can serve any sandbox. No range partitioning, no sticky routing, no per-namespace replicas.
- **Cross-cluster** — single K8s cluster only.
- **HA storage for the runtime ConfigMap** — already cluster-stored, no change.
- **Distributed tracing across replicas** — telemetry stays per-event, not per-request-trace. Add later if needed.
- **Graceful drain** — when a replica shuts down, in-flight HTTP requests are killed by the LB's normal termination flow. The K8s sandboxes themselves are unaffected (they live in their own pods).
