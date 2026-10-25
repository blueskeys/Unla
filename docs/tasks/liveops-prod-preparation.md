# live-ops production 本地准备

用户已授权本地准备；不提交、部署、访问浏览器，不修改 Beta、ExAI 或 live-ops。生产可达性、运行版本、提交与 243 部署由主线程完成。

- [x] 从现有 Beta 精确提取五个已审核工具，生成独立生产 YAML。
- [x] 实现仅创建、不覆盖的可重入管理 API 注册脚本，使用既有 private env，校验活动完整版本。
- [x] 准备仅使用无效 fixture 凭证的拒绝与目录验收脚本，由主线程执行。
- [x] 运行配置、响应模板及注册脚本的最小本地测试，记录交付与阻断。

成功标准：default/liveops-prod、/gateway/liveops-prod、固定 HTTPS 上游、delegated_bearer 不变；五个工具描述与成功输出明确 production；无凭证、无额外工具；不一致配置拒绝，重复执行无写入，创建后 readback 核验。

## 文件与行为

- `deploy/ubuntu243/liveops-prod.yaml`：仅五个批准工具，固定 `https://ops.api.kindgem.net`。参数、请求头、方法、投影字段沿用当前 Beta；成功响应增加/改为 `environment=production`，描述明确生产并删除本批不存在的商品摘要能力引用。Beta 文件未变。
- `deploy/ubuntu243/register-liveops-prod.py`：沿用管理 API `http://192.168.100.243:8080` 和用户目录 `.config/unla243/runtime.env` 中既有管理登录凭据；这些凭据仅用于管理 API，不进入工具配置。拒绝 HTTP 重定向。仅首次 POST 创建，不执行 PUT/DELETE；已有一致配置只读核验后退出；已有不一致配置或前缀冲突拒绝。只允许已知 Go 默认字段补齐，额外代理/工具等仍拒绝。
- 管理列表 DTO 不返回 annotations/meta，因此还读取唯一活动版本，将 JSON 字符串字段解码后比较完整配置。创建后读回失败则非零退出，停止后续 ExAI 绑定，不自动覆盖或删除；主线程排查后可重入。API 成功但通知网关失败等情况仍需核对网关实际目录，数据库读回不等于数据面验收。
- `deploy/ubuntu243/verify-liveops-prod.py`：使用既有 `.config/unla243/tls/ca.crt` 校验 TLS；仅无效 fixture A/B。验收缺失认证 401、换 token 403、production 会话跨 Beta 前缀 403、恰好五工具且描述明确 production、无效凭证调用货盘列表返回 isError 且凭证不回显。finally 用原凭证 DELETE 会话；405 属于失败。不会创建 Beta 会话或使用真实用户凭据。
- `deploy/ubuntu243/test_liveops_prod.py`、`internal/core/liveops_prod_config_test.go`：离线模拟注册与验收，并使用真实 Go 配置校验器及模板渲染链核对 YAML 和投影。

## 本地验证命令与结果

从 Unla 根目录使用 PowerShell：

```powershell
python -B deploy/ubuntu243/register-liveops-prod.py --check
python -B -m unittest discover -s deploy/ubuntu243 -p test_liveops_prod.py -v
& .\temp\toolchain\go\bin\go.exe test ./internal/core -run 'Test(LiveopsProdConfig|Delegated)' -count=1
git diff --check
```

上述检查通过：7 项 Python 测试；Go 聚焦测试通过（4.028 秒）。覆盖创建后重入仅一次 POST、配置漂移与额外行为拒绝、完整版本 annotations 漂移、前缀冲突、readback 失败、验收失败仍关闭会话和 DELETE 405 拒绝。五工具用合成 fixture 对比 Beta/production 响应，除 environment 外相等，敏感哨兵字段不输出。仅添加 Go 测试，未修改服务源码，不需要镜像重建。

## 主线程执行

提交推送并在 243 拉取对应版本后，由主线程在具备上述 private env、CA 和 PyYAML 的执行环境运行（以下是待执行命令，本任务未执行）：

```powershell
python -B deploy/ubuntu243/register-liveops-prod.py
python -B deploy/ubuntu243/register-liveops-prod.py
python -B deploy/ubuntu243/verify-liveops-prod.py
```

第二次注册应报告无需变更，之后验收应全部通过。目标 MCP URL 为 `https://192.168.100.243:8443/gateway/liveops-prod/mcp`。失败时停止绑定，不自动改写生产或 Beta；网络超时后注册可能已经落库，必须先读回再判断。

主线程补充证据：生产 HEAD `3cd1ceed`、已跟踪文件干净，五路由公开 OpenAPI 存在；243 gateway 容器至生产 `/openapi.json` TLS 正常且 HTTP 200；Unla 线上源码 `25bbfb5`、运行 backend `3d584db`，无需镜像重建。本任务引用这些证据，未自行远程复核。

## Review

本地起始 HEAD `25bbfb5`，工作区干净；仅新增本任务文件及更新 todo/lessons，未修改 Beta、live-ops、ExAI 或凭证文件；未提交、部署、操作浏览器或启动后台服务。无本地阻断。生产注册与拒绝验收仍待主线程执行；无效凭证验收不能证明真实账号的数据范围、撤权窗口或真实业务成功调用。ExAI 精确生产 URL 的逐用户凭证注入与绑定也由主线程负责。
