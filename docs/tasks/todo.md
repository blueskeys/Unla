# delegated_bearer 局部保护

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
