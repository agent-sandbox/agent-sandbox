# CHANGELOG


V0.1.0 - 2026-01-06
- 项目初始版本发布。
--------------------------
V0.1.1 - 2026-01-07
- 新增 sandbox-template 的 startupProbe 配置，修复实例未就绪时偶发获取 IP 失败的问题。
--------------------------
V0.2.0 - 2026-01-27
- 支持 E2B 协议并兼容 SDK。
- 新增 E2B Code Interpreter 支持。
- 新增 E2B Desktop 支持（含 VNC 与 GUI 应用）。
- 支持基于超时机制的自动缩容。
--------------------------
V0.3.0 - 2026-03-03
- 新增 Sandbox Template Pool 功能，可预创建沙箱实例以加快分配速度。
- 新增动态 Sandbox Template，可按模板模式创建沙箱实例。
- 新增 OpenClaw 模板。
- 修复默认端口获取问题。
- 修复 httpServer WriteTimeout 配置问题。
--------------------------
V0.3.1 - 2026-03-12
- 新增从 ConfigMap 加载动态模板配置。
- 模板池支持 warmup 预热功能，可预先执行命令或脚本以保持沙箱预热并降低资源消耗。
- 新增供 Agent 使用的 skills。
--------------------------
V0.4.0 - 2026-03-19
- 新增沙箱管理 UI，可查看沙箱实例、模板和日志。
--------------------------
V0.4.1 - 2026-03-26
- 新增沙箱事件与指标能力，可监控沙箱实例状态与性能。
- 新增沙箱实例的模板资源配置。
- 新增模板 metadata 配置，可通过 go-template 格式按不同场景自定义 sandbox-template 配置（例如 #5）。
- 新增 sandbox-template 配置 UI，可在界面中查看和编辑 sandbox-template（ReplicaSet）配置。
--------------------------
V0.4.2 - 2026-04-02
- 新增：为 Template 新增 args 字段，并更新 TemplatesConfigPage 以支持 args 输入 #6
- 新增：基本的权限控制，区分sys和普通用户，sys用户可以管理所有的sandbox-template和sandbox实例以及使用配置功能，普通用户只能管理自己创建的sandbox实例， 默认sys用户是：sys-2492a85b10ed4cb083b2c76b181eac96
- 改进：通过本地候选集锁优化池获取性能，避免 ReplicaSet 更新冲突
- 改进：UI Sandboxes和Pool Sandboxes增加排序功能，默认按照创建时间排序
- 破坏性变更：移除以编程方式设置 sandbox-template labels，改为在 sandbox-template 中配置全部 labels，参考：[sandbox.yaml](config/sandbox.yaml)
--------------------------
V0.5.0 - 2026-05-19
- 新增：容量管理，支持设置全局容量限制和单个Token的容量限制，包括并行创建沙箱限制和沙箱总量的配额限制；
- 新增：极速启动模式，no-startupProbe+transport-dial-retry，达到不用Pool的也能极速启动的效果，约1s-3s启动一个沙箱；
- 新增：Idle Timeout机制，支持配置沙箱空闲超时时间，没有HTTP请求，没有Command Exec或Files等操作，自动回收空闲沙箱，`Sandbox.create(timeout=60*30,metadata={"idleTimeout":"300"})`；
- 新增：基础配置热更新，支持动态调整常用配置项，包括Token配置，默认模版名称配置，容量配置等；
- 改进：性能优化，调整client-go并发参数，以及全部用Informer代替Api Server查询，减少API Server的压力，提升并行性能，50并发创建200个沙箱平均2.8s；
- 改进：Sandbox环境变量，在Sandbox内通过环境变量获取Sandbox ID等信息；
- 变更：移除未使用的Template Resources配置；
--------------------------
V0.6.0 - 2026-06-03
- 新增：Pause/Resume功能，支持暂停和恢复沙箱实例，暂停后保留沙箱启动的后台命令快照，例如(`sbx = Sandbox.create(template="code-sandbox-template",timeout=50*60,lifecycle=SandboxLifecycle(on_timeout="pause", auto_resume=True)); sbx.commands.run("python -m http.server 8001", background=True, timeout=0)`)，恢复后自动拉起保留的后台命令快照；
- 新增：UI Dashboard，综合展示沙箱创建的状态；
- 改进：优化idle timeout回收性能，过滤掉不需要回收的沙箱；
--------------------------
V0.7.0 - 2026-06-24
- 新增：Sandbox Snapshot功能，可以将Sandbox 启动的命令进行Snapshot保存，并在恢复时自动拉起；
- 新增：Telemetry数据上报功能，支持上报沙箱的创建、销毁、错误等Log数据，便于监控和分析沙箱的使用情况；
--------------------------
V0.8.0 - 2026-07-10
- 新增：Sandbox Controller 支持Leader选举，以支持多实例（高可用）部署；集群级副作用（池补充、缩容）仅在被选举的Leader上运行，其余副本继续提供API与代理流量；池同步器与缩容器可在Leader重新选举后正确恢复：每个任期使用新的工作队列，并在补充前等待Informer缓存同步，避免过量创建池ReplicaSet；
- 新增：集群状态接口（`GET /api/v1/status`），返回整体状态；
- 新增：Sandbox Snapshot支持SDK操作，如：`sbx.create_snapshot(); sbx.delete_snapshot()`，在获取Sandbox详情时返回Snapshot的内容；
- 改进：Sandbox-Template 改为 Sandbox-Blueprint，避免跟Templates概念混淆；
--------------------------
