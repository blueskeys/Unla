# 只读业务路由的当前 Bearer 委托

仅对显式配置 `routers[].auth.mode: delegated_bearer` 的 HTTP 后端生效。配置示例（占位地址不能直接发布）：

```yaml
routers:
  - server: liveops-beta-readonly
    prefix: /liveops-beta
    auth:
      mode: delegated_bearer
servers:
  - name: liveops-beta-readonly
    allowedTools: [approved_read]
tools:
  - name: approved_read
    method: GET
    endpoint: http://REPLACE_WITH_APPROVED_HOST:9731/REPLACE_WITH_APPROVED_READ_PATH
    args:
      - name: page
        position: query
        type: integer
        default: "1"
```

这是认证模式及工具约束示例，不是实际业务 API 清单；真实地址、路径和响应投影必须由已批准配置指定。Beta 实际清单见 `deploy/ubuntu243/liveops-beta.yaml`。

## 凭证与会话

- 每个请求（包括 SSE 建连、消息、Streamable POST/GET/DELETE）必须携带当前 `Authorization: Bearer <token>`。重复头、缺失、空值和无效格式拒绝。此模式按服务端 MCP 客户端设计，OPTIONS 也不绕过检查。
- 初始化会话保存 SHA-256 token 摘要及原 prefix，不保存初始化 Headers/Query/Cookies。换 token 必须重新初始化会话；原会话不能跨 prefix，借用无认证路由亦不可绕过。
- 网关只检查格式及会话绑定，不验证签名、有效期或撤权。每次业务工具调用向 live-ops 发送当前 token，401/403 等 HTTP 失败保留为 MCP `isError`。没有上游调用的 ping/list/流建连不证明 token 有效。
- 受保护路由不执行模型参数 Header 转发；Authorization/Cookie 不进入模板上下文，发送前将 Authorization 强制设置为当前请求值，并移除 Cookie。不得再配置依赖 `Request.Headers.Authorization` 的模板。
- 保留部署已有 `forward.mcp_arg.key_for_header: ""`；此模式即使全局转发键非空也不执行参数转发。
- 关闭此模式下的工具参数、请求 Header、响应正文 Debug 与下游请求/响应内容 trace 捕获；拒绝上游正文直接回显当前 token。session 存储只含摘要，仍需按受保护元数据管理。

## 固定只读出口

执行前要求工具方法为 GET/HEAD，endpoint 为固定 HTTP(S) origin，不含 userinfo/fragment。允许 query 参数，以及完整路径段形式的 `{{.Args.identifier}}`：必须声明为 `position: path, required: true, type: string`，原始实参匹配 `^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`；缺失不能回退默认值。主机、端口、query 和部分路径段均不接受模板。不允许请求正文、工具代理或重定向。allowedTools 继续控制工具发现与执行。非 JSON/文本响应拒绝。

GET/HEAD 不证明业务无副作用；仅配置经确认只读的 API。目标没有硬编码为 live-ops 主机，管理配置和网络出口仍是信任边界。不得开放任意模型 URL 或带写入语义的 GET API。

## HTTP 预算

所有 HTTP 工具共享常量：总 Context 预算及 HTTP Client Timeout 30 秒；传输体、每层解压体和普通工具结果各最多 4 MiB；`delegated_bearer` 工具序列化结果进一步限制为 1 MiB，与 ExAI 的结果预算一致，超限返回工具错误，不静默截断，也不把过大的结果发给客户端触发整连接关闭。非 identity 编码最多 3 层。读取最多多探测 1 字节后拒绝，不依赖 Content-Length。禁用 Transport 隐式解压，由受限读取流程解码；zstd 窗口上限 8 MiB、内存参数上限 16 MiB、单解码并发。gzip/deflate/brotli 沿用库自身有界窗口。

请求取消/更短的调用方 deadline 优先。读取与逐层解压检查 Context，转换后再次检查；同步模板函数不能被 Context 强行抢占，结果限额也不是模板内部临时分配限额。只使用经过审核的固定模板，本次未扩展通用模板沙箱。

## 本地验证

测试只用 `httptest` 本地模拟服务，不访问业务数据。聚焦 `internal/core` 的 delegated、HTTP 预算、已有执行/解码及 Streamable 测试，并编译 `cmd/mcp-gateway`。具体执行结果记录在 `docs/tasks/todo.md`。
