# TODO
1. - [x] E2B Protocol support API,SDK Compatibility, use examples from E2B official.
1. - [x] E2B Desktop support VNC, GUI applications support.
1. - [x] Use configmap to configure sandbox-manager
1. - [x] Scale-down by timeout
1. - [x] Scale-down by unused time (idle timeout)
1. - [x] Sandbox-Pool
1. - [x] UI Dashboard
1. - [x] Pause and Resume
1. - [x] Sandbox proxy(router) rate limiting
1. - [x] Metrics exporting
1. - [x] Leader election for high availability
1. - [x] Sandbox Snapshot
1. - [x] Use lease instead event to optimize idle timeout performance
1. - [x] Telemetry data reporting by logs
1. - [ ] Resources reservation
1. - [ ] Update Sandbox config e.g. resources, env, args, etc. by create with sandbox id, e.g. create(metadata:{sandbox_id:xxx}).
1. - [ ] Audit for cmd or file ops
1. - [ ] Events list optimize
1. - [ ] return events for get info
1. - [ ] Treat API keys with and without the e2b_ prefix as the same user
1. - [ ] Use merge patch instead of full update where no optimistic lock is needed: DeleteProcessSnapshot, pod labels sync, active lease renew
1. - [ ] Dispatch by host before path routing: requests to sandbox subdomains (`{port}-{id}.{domain}`) should go straight to `SandboxRouterOfDomain`, skipping platform routes and API-key auth. Today host-less routes (`/api/v1/...`, `/ui/`, `/mcp`, `/e2b/v1/...`, `/healthz`) shadow the same paths served by apps inside the sandbox.
1. - [ ] envd init failure handling: make it mandatory for sandboxes created via the E2B API/SDK (fail create/resume, since the SDK depends on envd and its access token), keep it best effort for other APIs. Also keep `sandbox.create`/`sandbox.resume` telemetry success unaffected by best-effort failures (e.g. use a separate `sandbox.envd_init` event).
1. - [ ] E2B sandbox state handling: the E2B API only knows `running`/`paused`, but the internal `creating` state is returned as-is, so SDK `Sandbox.list()` raises `ValueError` on it. (1) On E2B create failure, delete the sandbox (E2B semantics: a failed create leaves nothing behind; today a timed-out create leaves the ReplicaSet stuck in `creating`, still counting against the user's capacity). (2) Filter `creating` out of E2B list results instead of mapping it to `running`. (3) Keep internal states in UI/REST for troubleshooting; optionally add a `failed` state derived from pod status (ImagePullBackOff, CrashLoopBackOff). Decide whether (1) also applies to the REST API.
