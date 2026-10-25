import argparse
import copy
import json
from pathlib import Path
import sys
import urllib.request

import yaml

BASE = "http://192.168.100.243:8080"
TARGET = Path(__file__).with_name("liveops-prod.yaml")
PRIVATE_ENV = Path.home() / ".config/unla243/runtime.env"
NAMES = ["liveops_list_boards", "liveops_get_pallet_workspace",
         "liveops_list_pallet_sessions", "liveops_get_session_assortment", "liveops_list_replays"]


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


def verify_saved(saved, desired, call, headers):
    if normalize(saved, listed=True) != normalize(desired, listed=True):
        raise RuntimeError("现有生产配置不一致，拒绝覆盖")
    versions = call("GET", "/api/mcp/configs/versions?tenant=default&names=liveops-prod", headers=headers)["data"]
    active = [v for v in versions or [] if v["is_active"] and v["name"] == "liveops-prod" and v["tenant"] == "default"]
    if len(active) != 1:
        raise RuntimeError("无法唯一确定生产活动版本")
    full = {"name": "liveops-prod", "tenant": "default"}
    for key, field in (("routers", "routers"), ("servers", "servers"), ("tools", "tools"), ("prompts", "prompts"), ("mcpServers", "mcp_servers")):
        full[key] = json.loads(active[0][field])
    if normalize(full) != normalize(desired):
        raise RuntimeError("生产活动版本完整契约不一致，拒绝覆盖")


def register(payload, desired, headers, call=request):
    configs = call("GET", "/api/mcp/configs", headers=headers)["data"]
    existing = [c for c in configs if c["name"] == "liveops-prod" and c["tenant"] == "default"]
    if len(existing) > 1:
        raise RuntimeError("生产配置不唯一")
    if existing:
        verify_saved(existing[0], desired, call, headers)
        return "无需变更：生产配置及活动完整版本一致"
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
    parser = argparse.ArgumentParser(description="仅创建生产五工具配置；不一致则拒绝，不执行更新或删除")
    parser.add_argument("--check", action="store_true", help="仅检查本地 YAML，不读取凭证、不访问网络")
    args = parser.parse_args()
    payload, desired = load_target()
    if args.check:
        print("通过：本地 production 五工具 YAML 基本约束")
        return
    env = dict(line.split("=", 1) for line in PRIVATE_ENV.read_text().splitlines() if "=" in line and not line.startswith("#"))
    login = request("POST", "/api/auth/login", json.dumps({"username": env["SUPER_ADMIN_USERNAME"], "password": env["SUPER_ADMIN_PASSWORD"]}).encode(), {"Content-Type": "application/json"})
    headers = {"Authorization": "Bearer " + login["token"], "Content-Type": "application/yaml"}
    print(register(payload, desired, headers))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print("注册失败，停止后续绑定：" + (str(error) if isinstance(error, RuntimeError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
