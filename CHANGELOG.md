# CHANGELOG


V0.1.0 - 2026-01-06
- Initial release of the project.
-------------------------- 
V0.1.1 - 2026-01-07
- Add sandbox template startupProbe config, fix get instance ip failed when it not ready in sometimes.
-------------------------- 
V0.2.0 - 2026-01-27
- Support E2B Protocol with SDK compatibility.
- Add E2B Code-interpreter support.
- Add E2B Desktop support with VNC and GUI applications.
- Support scale-down by timeout mechanism.
-------------------------- 
V0.3.0 - 2026-03-03
- Add Sandbox template Pool feature, which can pre-create sandbox instances for faster allocation.
- Add dynamic Sandbox template, which can create sandbox instances with template by pattern.
- Add OpenClaw template.
- Fix get default port bug.
- Fix httpServer WriteTimeout config bug.
-------------------------- 
V0.3.1 - 2026-03-12
- Add dynamic templates config load from configmap.
- Template pool support warmup feature, which can pre-run some commands or scripts to keep the sandbox instance warmup and low  resource consumption.
- Add skills for agent use.
-------------------------- 
V0.4.0 - 2026-03-19
- Add UI for sandbox management, which can view sandbox instances, templates, and logs.
-------------------------- 
V0.4.1 - 2026-03-26
- Add sandboxes events and metrics, which can monitor the status and performance of sandbox instances.
- Add template resources config for sandbox instance.
- Add template metadata config, which can customize sandbox-template specific config for different use cases with go-template format, e.g. #5.
- Add UI for sandbox-template config, which can view and edit the sandbox-template(ReplicasSet) config in UI.
--------------------------
V0.4.2 - 2026-04-02
- Add: args field to Template, and update TemplatesConfigPage to support args input #6.
- Add: basic access control to separate system and regular users; system users can manage all sandbox templates and sandbox instances and use configuration features, while regular users can only manage sandbox instances they created. The default system user is: `sys-2492a85b10ed4cb083b2c76b181eac96`.
- Improve: enhance pool acquisition performance by using a local candidate lock to avoid ReplicaSet update conflicts.
- Improve: add sorting for Sandboxes and Pool Sandboxes in UI, with default ordering by creation time.
- Breaking change: remove programmatic label assignment for sandbox-template; configure all labels directly in sandbox-template **update it before upgrade this version** . Reference: [sandbox.yaml](config/sandbox.yaml).
--------------------------
V0.5.0 - 2026-05-19
- Add: capacity management, with support for global capacity limits and per-token capacity limits, including concurrent sandbox creation limits and total sandbox quota limits.
- Add: fast startup mode with no-startupProbe and transport dial retry, enabling fast sandbox startup without using a pool, typically starting a sandbox in about 1-3 seconds.
- Add: idle timeout mechanism, supporting sandbox idle timeout configuration. Sandboxes with no HTTP requests, command exec, files, or other operations are automatically reclaimed, for example: `Sandbox.create(timeout=60*30, metadata={"idleTimeout":"300"})`.
- Add: runtime configuration hot reload, supporting dynamic updates for common configuration items including token config, default template name config, and capacity config.
- Improve: performance optimization by tuning client-go concurrency parameters and replacing API server queries with informers to reduce API server pressure and improve parallel performance. Creating 200 sandboxes with concurrency 50 averages 2.8 seconds.
- Improve: sandbox environment variables, allowing sandbox ID and related information to be accessed from inside the sandbox through environment variables.
- Change: remove unused Template Resources config.
--------------------------
V0.6.0 - 2026-06-03
- Add: Pause/Resume feature, supporting pausing and resuming sandbox instances. When paused, background command snapshots are preserved (e.g., `sbx = Sandbox.create(template="code-sandbox-template", timeout=50*60, lifecycle=SandboxLifecycle(on_timeout="pause", auto_resume=True)); sbx.commands.run("python -m http.server 8001", background=True, timeout=0)`). Upon resume, preserved background command snapshots are automatically re-start.
- Add: UI Dashboard, providing a comprehensive view of sandbox creation status.
- Improve: optimize idle timeout reclamation performance by filtering out sandboxes that don't need reclamation.
--------------------------
V0.7.0 - 2026-06-24
- Add: Sandbox Snapshot feature, which can snapshot the commands started in a sandbox and automatically re-start them on resume.
- Add: Telemetry reporting feature, supporting reporting of sandbox creation, destruction, error, and other log data, making it easier to monitor and analyze sandbox usage.
--------------------------
V0.8.0 - 2026-07-10
- Add: Leader election for the Sandbox Controller to support multi-instance (HA) deployment. Pool replenishment and scaler runs only on the leader, while every replica keeps serving API and proxy traffic. 
- Add: cluster status endpoint (`GET /api/v1/status`) return whole base info.
- Add: Sandbox Snapshot SDK operations, e.g. `sbx.create_snapshot(); sbx.delete_snapshot()`; the snapshot content is returned when fetching sandbox info.
- Change: rename Sandbox-Template to Sandbox-Blueprint to avoid confusion with the Templates concept.
--------------------------
