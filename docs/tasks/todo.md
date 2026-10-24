# delegated_bearer 局部保护

## 后续任务：live-ops 接口梳理与挂载

- [x] 确认原实现保留：工作区干净，HEAD 为 `7c09604`，无需再次恢复文件。
- [x] 静态梳理 live-ops 实际注册路由，区分读写、权限和适配限制；不导入服务或访问数据库。
- [x] 核对首批只读接口的参数、响应与副作用，准备 Unla 挂载配置及本地验证。
- [x] 记录待批准或受当前路由限制的接口，不将静态验证表述为远程挂载成功。

用户最新授权开始实施：梳理货盘表、商品表、直播监控和商品中心的只读接口，注册到现有 Unla 并绑定既有 ExAI 智能体。沿用 Beta 9731、智能体 `639982cf-e4fe-40f5-b4c9-44296cd7d678`、逐用户凭证和现有保护，不修改 live-ops，不开放写操作。以下为当前实施计划：

- [x] 核对本地与 Beta 已部署版本，按接口分别验证只读语义、权限和字段。
- [x] 必要时最小支持受约束的路径 ID，保持固定上游和认证；验证越界输入、配置与响应投影。
- [x] 经本机提交推送和远端 Git 更新发布 Unla，备份并更新原配置，不重复创建。
- [x] 通过 ExAI 正式接口发现、发布并更新既有绑定，验证新目录和实际只读调用。
- [x] 记录实际版本、测试证据、未开放接口和恢复步骤。

本次扩展 Review：Unla运行 `7888252`；ExAI MCP v2、既有智能体revision2。9工具均有真实成功调用证据，首轮并发传输/门禁失败后串行复验成功，原因仍待定位；不宣称并发稳定或双真实账号隔离已验收。完整接口、边界、Run及恢复步骤见 `docs/liveops-beta-read-tools.md`。未修改live-ops或ExAI源码。

下面为原凭证保护子任务历史记录，其“不部署”限制不作为本次新增实施的状态描述。

用户已授权本地实施与测试；不修改 ExAI、deploy 或 live-ops，不提交、推送、部署，不访问服务器或业务数据。

- [x] 阅读 agents.md、后端 SOP 和指定认证核查文档。
- [x] 新增显式 delegated_bearer 模式，当前凭证检查、会话摘要与 prefix 绑定、可信注入和固定只读出口。
- [x] 增加 HTTP 总预算与传输/解压有限读取。
- [x] 本地模拟服务测试认证隔离、参数覆盖、响应限额、超时及旧路由兼容，编译网关。

设计：仅显式路由启用认证变化；会话不保存 Bearer 原文；上游只允许配置的字面量 HTTP(S) URL、GET/HEAD 和 query 参数，禁止重定向。HTTP 工具共用 30 秒、4 MiB、最多 3 层压缩的安全常量。过期/撤销与对象权限交由 live-ops 验证。

## Review

2026-10-24 本地验证完成：

- Go 1.25.0 工具链位于已忽略的 `temp/toolchain/go`；初次测试因解压尚未完成而报标准库 `time` 缺失，待解压完成后同范围重跑通过。
- `go test ./internal/core -run 'Test(Delegated|HTTPBodyBudget|HTTPDecoderLimits|HTTPToolDeadline|ExecuteHTTPTool|ReadDecodedResponseBody|ModernStreamable)' -count=1`：通过，6.102 秒。
- `go build -o temp/mcp-gateway.exe ./cmd/mcp-gateway`：通过。
- `git diff --check`：通过，仅 Git CRLF 提示；未运行全量测试，未启动持久服务。
- 新增测试覆盖缺失/重复/非法 Authorization、换 token、跨受保护/开放 prefix、调用/流/删除/SSE 消息、会话序列化无原始凭证、当前请求强制注入、伪造 forward 参数、上游 401、固定只读工具约束、禁止重定向、4 MiB 边界、gzip/br/zstd/deflate/raw-deflate 解压超限、多层中间结果超限，以及响应头/正文 deadline。

范围和风险：只改 Unla 本地源码、测试及文档；没有提交、推送、部署或访问业务服务。工作区另外出现的 `deploy/ubuntu243/` 文件非本任务写入，保持原样。精确启用配置和限制见 `docs/delegated-bearer.md`。真实凭证兼容性、对象权限和撤权时效尚需业务联调；GET/HEAD 的实际只读语义仍须审核。同步模板计算不能强行抢占，模板临时分配不受结果限额保护；本次不扩展模板平台。
