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
