"""串行扩展既有 Beta 配置，备份并保留旧工具契约，不输出凭证。"""
import json
import os
from pathlib import Path
import subprocess
import urllib.request

import yaml

BASE = "http://192.168.100.243:8080"
TARGET = Path(__file__).with_name("liveops-beta.yaml")
env = dict(line.split("=", 1) for line in (Path.home() / ".config/unla243/runtime.env").read_text().splitlines() if "=" in line and not line.startswith("#"))


def request(method, path, payload=None, headers=None):
    req = urllib.request.Request(BASE + path, data=payload, method=method, headers=headers or {})
    with urllib.request.urlopen(req, timeout=30) as response:
        return json.load(response)


login = request("POST", "/api/auth/login", json.dumps({"username": env["SUPER_ADMIN_USERNAME"], "password": env["SUPER_ADMIN_PASSWORD"]}).encode(), {"Content-Type": "application/json"})
headers = {"Authorization": "Bearer " + login["token"], "Content-Type": "application/yaml"}
configs = request("GET", "/api/mcp/configs", headers=headers)["data"]
old = next(item for item in configs if item["name"] == "liveops-beta" and item["tenant"] == "default")
desired = yaml.safe_load(TARGET.read_text())
baseline = yaml.safe_load(subprocess.check_output(["git", "show", "7c09604:deploy/ubuntu243/liveops-beta.yaml"], text=True))


def same_contract(actual, expected):
    """管理 DTO 会补空默认字段，仅比较配置中明确声明的值。"""
    if isinstance(expected, dict):
        return isinstance(actual, dict) and all(key in actual and same_contract(actual[key], value) for key, value in expected.items())
    if isinstance(expected, list):
        return isinstance(actual, list) and len(actual) == len(expected) and all(same_contract(a, b) for a, b in zip(actual, expected))
    return actual == expected


old_tools = {tool["name"]: tool for tool in old["tools"]}
new_tools = {tool["name"]: tool for tool in desired["tools"]}


def listed_contract(cfg):
    """当前管理列表 DTO 不返回 annotations/meta，实际 MCP 目录另行核验。"""
    visible = json.loads(json.dumps(cfg))
    for tool in visible.get("tools", []):
        tool.pop("annotations", None)
        tool.pop("meta", None)
    return visible


visible_desired = listed_contract(desired)
visible_tools = {tool["name"]: tool for tool in visible_desired["tools"]}
assert same_contract(old["routers"], baseline["routers"]), "Existing router changed; inspect before updating"
assert same_contract(new_tools["liveops_list_boards"], baseline["tools"][0]), "Existing tool contract must remain unchanged"
assert set(old_tools).issubset(new_tools), "Unexpected existing tools; refusing replacement"
for name, tool in old_tools.items():
    assert same_contract(tool, visible_tools[name]), "Existing tool differs; inspect before updating: " + name
assert all(tool["method"] == "GET" for tool in desired["tools"])
if set(old_tools) == set(new_tools) and same_contract(old["servers"], desired["servers"]):
    print("No change: existing Beta configuration already matches")
else:
    backup = Path.home() / ".config/unla243/liveops-before-read-expansion.json"
    if not backup.exists():
        fd = os.open(backup, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as stream:
            json.dump(old, stream, ensure_ascii=False)
    request("PUT", "/api/mcp/configs", TARGET.read_bytes(), headers)
    saved = next(item for item in request("GET", "/api/mcp/configs", headers=headers)["data"] if item["name"] == "liveops-beta" and item["tenant"] == "default")
    assert same_contract(saved, visible_desired), "Saved configuration mismatch; keep ExAI binding unchanged"
    print("Updated default/liveops-beta: " + str(len(new_tools)) + " approved read-only tools; previous configuration backed up")
