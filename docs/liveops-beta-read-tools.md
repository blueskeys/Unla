# live-ops Beta 查询工具扩展

日期：2026-10-24。使用现有 `default/liveops-beta`、Beta 9731 和逐用户 Bearer，不修改 live-ops。

## 接口清单

所有工具仅 GET，固定上游 `http://100.92.212.67:9731`。具体参数及输出白名单以 `deploy/ubuntu243/liveops-beta.yaml` 为准。

| 工具 | 路径 | 数据范围和限制 |
| --- | --- | --- |
| liveops_list_boards | /api/boards | 当前用户货盘；保持旧版前五条与搜索契约 |
| liveops_get_board_goods | /api/boards/{board_id} | 指定有权限货盘在用商品；不返回回收站、已移除商品、成本、图片；无分页 |
| liveops_get_pallet_workspace | /api/boards/{board_id}/pallet-workspace | 已有工作空间，不自动创建 |
| liveops_list_pallet_sessions | /api/boards/{board_id}/pallet-sessions | 已存计划；必填日期，服务端日期差至多90天 |
| liveops_get_session_assortment | /api/boards/{board_id}/pallet-sessions/{session_id}/assortment | 场次成员和版本；不含商品主档，not_generated不等于确认零商品 |
| liveops_get_pallet_product | /api/boards/{board_id}/pallet-products/{goods_id} | 商品生命周期；已移除历史成员可能查不到 |
| liveops_list_replays | /api/live-monitor/replays | 有权访问货盘最近20条归档摘要，无翻页；不是当前开播状态 |
| liveops_list_products | /api/product-library/products | 全局商品中心摘要，独立product_library查询权限，固定每页20条 |
| liveops_get_product_workspace | /api/product-library/workspace | 全局共享单品和商品集摘要，不按创建者隔离；无分页 |

货盘类沿用 `live_ops:pallet:query` 和部门/对象校验；复盘沿用 `live_ops:live_monitor:query`。商品中心不冒充用户货盘范围。无分页响应超过4MiB直接失败，不静默裁剪后声称完整。

商品表 GET 会生成已有图片的本地派生缩略图缓存；无业务表写入或外部采集。本次按业务只读纳入，不能承诺底层零文件变化。上游仍会加载被输出投影去除的字段，因此投影不降低上游计算和读取成本。

## 排除范围

旧 `/workspace`、`/pallet-timeline` 会自动维护业务数据，排除。监控 `/sessions` 缺少功能权限映射，排除；客户端状态、事件订阅、readiness、开始/停止监控不开放。复盘详情路径编码及权限门禁未验收，暂缓。商品中心详情、更新批次、同步及所有写入/采集入口不开放。供应链保持暂缓。

## 兼容性及验证

本地业务源码 `c776922`；历史 Beta 部署 `ffc127e5`。已核对部署版本对应候选路由/查询服务与本地差异，并检查 Beta OpenAPI 存在候选 GET。发现的 live-monitor 差异为未使用的 SSE 事件实现，不以本地 HEAD 替代远端部署版本。

新增完整路径段 ID 模板，执行前严格验证原始必填字符串；保持固定 origin、当前 Bearer、session token摘要/prefix绑定及禁转发覆盖。Go1.25.0 聚焦 delegated、配置模板、响应预算、解压限额及deadline测试通过（4.532秒），网关编译通过。Python部署脚本语法检查及 `git diff --check` 通过。未运行全量测试。

## 实际发布与验收

- Unla 运行提交：`78882529aba553759abe825f4ebf45bf3381a6b5`，已推送 `beta`，243通过Git快进更新并本机构建。后端镜像 `unla243-backend:7888252`，摘要 `sha256:98c22cba382a690f95818491602265d662d4b82452df325ee1e80fa9552733db`。Web复用原镜像内容。
- 两份SQLite通过在线backup API备份并完整性检查，目录 `/home/ubuntu/.config/unla243/before-7888252`；管理配置备份 `~/.config/unla243/liveops-before-read-expansion.json`。文件0600。临时构建代理隧道已关闭，10809无监听。
- 注册9工具成功。远端拒绝测试通过：缺Bearer401、换token/跨prefix403、无效上游凭证isError、目录与配置一致。
- 协议目录保留 `readOnlyHint=true`、`idempotentHint=true`，并补默认 `openWorldHint=true`；现有Go模型使用omitempty，`destructiveHint=false`在传输中省略，不能声称wire与YAML逐字段完全相同。这些提示不作为授权机制，实际只读由GET精确路径配置强制，ExAI发布风险显式为read。
- ExAI MCP `4a69a354-2e60-49c9-acf3-c00e65bd738f` 发布v2；既有智能体 `639982cf-e4fe-40f5-b4c9-44296cd7d678` 更新revision2，绑定全部9工具并更新提示。保留execution、native_tools、其他绑定、名称及非默认状态；不创建重复配置，不改ExAI源码或重启ExAI。
- 真实会话 `137f9b4a-3ad9-4748-a5be-098406342348`。首轮Run `19364176-aa27-491c-b819-9670ca3e7d8b`：货盘列表、工作空间、计划场次和商品中心工作区成功；商品摘要和复盘传输失败，商品分页被执行前门禁拦截。
- 串行复验Run `fb00b2b6-4e75-49e9-a724-fc30d1d702e7`：商品摘要、复盘、商品分页、生命周期全部 `is_error=false`，分别10606/17471/1848/1728毫秒。不能把首轮并发故障解释为已定位或已修复；未改权限或改共享身份绕过。业务明细只保留在授权会话，不复制到仓库。
- 上播表补验Run `ceda9711-f0f2-4de9-917b-ae651bdba199`：查询已有日期范围取得真实场次ID，上播表工具 `is_error=false`、658毫秒。未创建或维护场次。至此9个工具均有真实调用成功证据，但不宣称并发稳定性已验收。

静态与fixture测试不等于真实权限隔离验收；尚无第二个真实受限账号隔离测试。Unla响应预算4MiB，ExAI还有更小的1MiB消息预算，达到任一限制都可能失败。上游无分页的大响应与较慢查询仍是限制。

## 恢复

优先通过ExAI正式API把该智能体绑定恢复到现有MCP v1（先读取最新revision，保留其他字段），不要删除历史版本。若须回退网关，使用已保留的 `unla243-backend:7c09604` 及原Web镜像；先把新增工具配置恢复为Git中 `7c09604:deploy/ubuntu243/liveops-beta.yaml`，再切换旧网关，避免旧版拒绝路径模板。不盲目覆盖备份以来的其他配置或数据库，不撤回既有认证保护。
