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
