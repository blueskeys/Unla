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

实际发布、绑定和业务验收状态另行记录；静态与fixture测试不等于真实权限隔离验收。
