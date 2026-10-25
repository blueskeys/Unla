import argparse
import copy
import json
import os
from datetime import datetime, timezone
from pathlib import Path
import subprocess
import sys
import urllib.request

import yaml

BASE = "http://192.168.100.243:8080"
TARGET = Path(__file__).with_name("liveops-prod.yaml")
PRIVATE_ENV = Path.home() / ".config/unla243/runtime.env"
NAMES = [
    "liveops_list_boards",
    "liveops_get_pallet_workspace",
    "liveops_list_pallet_sessions",
    "liveops_get_session_assortment",
    "liveops_get_pallet_product",
    "liveops_list_replays",
    "liveops_list_products",
    "liveops_get_product_workspace",
    "liveops_get_board_ai_prompt",
    "liveops_get_board_version",
    "liveops_list_board_trash",
    "liveops_list_legacy_assortments",
    "liveops_get_latest_legacy_assortment",
    "liveops_get_board_pending",
    "liveops_get_board_live_items_status",
    "liveops_get_session_live_items_status",
    "liveops_get_goods_crowd_profile",
    "liveops_list_board_crowd_profile_statuses",
    "liveops_get_goods_title_context",
    "liveops_list_board_title_contexts",
    "liveops_get_public_live_config",
    "liveops_get_auto_history_capture_status",
    "liveops_get_session_sync_state",
    "liveops_get_session_sync_account",
    "liveops_list_session_sync_pending",
    "liveops_list_session_sync_alerts",
    "liveops_list_session_recovery_tasks",
    "liveops_get_live_monitor_readiness",
    "liveops_list_selection_requests",
    "liveops_get_selection_schedule_summary",
    "liveops_get_selection_request",
    "liveops_list_hot_orders",
    "liveops_get_hot_order",
    "liveops_get_hot_order_goods_fact",
    "liveops_list_new_product_submit_links",
    "liveops_get_case_package_identity",
    "liveops_list_case_package_products",
    "liveops_list_product_cases",
    "liveops_get_product_other_media",
    "liveops_list_case_package_feed",
    "liveops_get_global_other_media",
    "liveops_list_case_feedback_tags",
    "liveops_get_case_feedback_tag_catalog",
    "liveops_list_case_feedback",
    "liveops_list_case_feedback_media",
    "liveops_get_case_package_index",
    "liveops_get_case_package",
    "liveops_export_selection_csv",
    "liveops_export_hot_orders_csv",
    "liveops_get_latest_data_update_batch",
    "liveops_get_data_update_batch",
    "liveops_get_product_set",
    "liveops_get_product_detail",
    "liveops_list_douyin_daily_plans",
    "liveops_get_douyin_crawl_batch",
    "liveops_list_refresh_jobs",
    "liveops_get_fxg_catalog_status",
    "liveops_get_provider_execution_settings",
    "liveops_get_client_update_metadata",
]


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise RuntimeError("管理 API 重定向被拒绝")


def request(method, path, payload=None, headers=None):
    req = urllib.request.Request(BASE + path, data=payload, method=method, headers=headers or {})
    with urllib.request.build_opener(NoRedirect()).open(req, timeout=30) as response:
        return json.load(response)


def normalize(config, listed=False):
    result = copy.deepcopy(config)
    for key in ("createdAt", "updatedAt"):
        result.pop(key, None)
    for key in ("prompts", "mcpServers"):
        if result.get(key) in (None, []):
            result.pop(key, None)
    for router in result.get("routers", []):
        router.setdefault("ssePrefix", "")
    for tool in result.get("tools", []):
        tool.setdefault("requestBody", "")
        if listed:
            tool.pop("annotations", None)
            tool.pop("meta", None)
        for arg in tool.get("args", []):
            arg.setdefault("required", False)
            arg.setdefault("default", "")
            arg.setdefault("items", {"type": ""})
    return result


def load_target():
    payload = TARGET.read_bytes()
    desired = yaml.safe_load(payload)
    if desired["name"] != "liveops-prod" or desired["tenant"] != "default":
        raise RuntimeError("生产配置身份不符")
    if desired["routers"] != [{"server": "liveops-prod", "prefix": "/gateway/liveops-prod", "auth": {"mode": "delegated_bearer"}}]:
        raise RuntimeError("生产路由保护不符")
    if [t["name"] for t in desired["tools"]] != NAMES or desired["servers"][0]["allowedTools"] != NAMES:
        raise RuntimeError("生产工具范围不符")
    for tool in desired["tools"]:
        if tool["method"] != "GET" or not tool["endpoint"].startswith("https://ops.api.kindgem.net/api/"):
            raise RuntimeError("生产只读上游不符")
        if "production" not in tool["description"] or '"environment"' not in tool["responseBody"] or "production" not in tool["responseBody"]:
            raise RuntimeError("生产标识缺失")
    if "beta" in payload.decode().lower() or "9731" in payload.decode():
        raise RuntimeError("生产配置残留验收环境标识")
    return payload, desired


def active_version(call, headers, name="liveops-prod"):
    versions = call("GET", "/api/mcp/configs/versions?tenant=default&names=" + name, headers=headers)["data"]
    active = [v for v in versions or [] if v["is_active"] and v["name"] == name and v["tenant"] == "default"]
    if len(active) != 1:
        raise RuntimeError("无法唯一确定活动版本：" + name)
    return active[0]


def full_config(version):
    full = {"name": "liveops-prod", "tenant": "default"}
    for key, field in (("routers", "routers"), ("servers", "servers"), ("tools", "tools"), ("prompts", "prompts"), ("mcpServers", "mcp_servers")):
        full[key] = json.loads(version[field])
    return full


def verify_saved(saved, desired, call, headers):
    if normalize(saved, listed=True) != normalize(desired, listed=True):
        raise RuntimeError("现有生产配置不一致，拒绝覆盖")
    full = full_config(active_version(call, headers))
    if normalize(full) != normalize(desired):
        raise RuntimeError("生产活动版本完整契约不一致，拒绝覆盖")


def backup_version(saved, version):
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    path = PRIVATE_ENV.parent / ("liveops-prod-before-expansion-" + stamp + ".json")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        json.dump({"configuration": saved, "active_version": version}, stream, ensure_ascii=False)
    return path


def register(payload, desired, headers, call=request, baseline=None, backup=backup_version):
    configs = call("GET", "/api/mcp/configs", headers=headers)["data"]
    existing = [c for c in configs if c["name"] == "liveops-prod" and c["tenant"] == "default"]
    if len(existing) > 1:
        raise RuntimeError("生产配置不唯一")
    if existing:
        version = active_version(call, headers)
        if normalize(full_config(version)) == normalize(desired):
            verify_saved(existing[0], desired, call, headers)
            return "无需变更：生产配置及活动完整版本一致"
        if baseline is None:
            raise RuntimeError("生产配置有差异，未提供受控更新基线")
        verify_saved(existing[0], baseline, call, headers)
        beta_before = active_version(call, headers, "liveops-beta")
        backup_path = backup(existing[0], version)
        call("PUT", "/api/mcp/configs", payload, headers)
        saved = [c for c in call("GET", "/api/mcp/configs", headers=headers)["data"] if c["name"] == "liveops-prod" and c["tenant"] == "default"]
        if len(saved) != 1:
            raise RuntimeError("更新后生产配置不唯一，停止后续绑定")
        verify_saved(saved[0], desired, call, headers)
        if active_version(call, headers, "liveops-beta") != beta_before:
            raise RuntimeError("Beta 活动版本发生变化，停止后续绑定")
        return "生产配置已更新；完整活动版本与 Beta 不变核验通过；备份：" + str(backup_path)
    for config in configs:
        for router in config.get("routers", []):
            prefix = router["prefix"].rstrip("/")
            target = "/gateway/liveops-prod"
            if prefix == target or target.startswith(prefix + "/") or prefix.startswith(target + "/"):
                raise RuntimeError("生产前缀已被其他配置占用")
    call("POST", "/api/mcp/configs", payload, headers)
    saved = [c for c in call("GET", "/api/mcp/configs", headers=headers)["data"] if c["name"] == "liveops-prod" and c["tenant"] == "default"]
    if len(saved) != 1:
        raise RuntimeError("创建后无法唯一读回生产配置；停止后续绑定")
    verify_saved(saved[0], desired, call, headers)
    return "已创建 default/liveops-prod；列表及活动完整版本 readback 通过"


def main():
    parser = argparse.ArgumentParser(description="创建或受控扩展生产配置；仅接受已审核旧基线，先完整备份再更新")
    parser.add_argument("--check", action="store_true", help="仅检查本地 YAML，不读取凭证、不访问网络")
    args = parser.parse_args()
    payload, desired = load_target()
    if args.check:
        print("通过：本地 production 工具 YAML 基本约束，共 " + str(len(NAMES)) + " 项")
        return
    env = dict(line.split("=", 1) for line in PRIVATE_ENV.read_text().splitlines() if "=" in line and not line.startswith("#"))
    login = request("POST", "/api/auth/login", json.dumps({"username": env["SUPER_ADMIN_USERNAME"], "password": env["SUPER_ADMIN_PASSWORD"]}).encode(), {"Content-Type": "application/json"})
    headers = {"Authorization": "Bearer " + login["token"], "Content-Type": "application/yaml"}
    baseline = yaml.safe_load(subprocess.check_output(["git", "show", "75bc642d0e4d6815c52b080ae377b832036da197:deploy/ubuntu243/liveops-prod.yaml"], cwd=TARGET.parents[2]))
    print(register(payload, desired, headers, baseline=baseline))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print("注册失败，停止后续绑定：" + (str(error) if isinstance(error, RuntimeError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
