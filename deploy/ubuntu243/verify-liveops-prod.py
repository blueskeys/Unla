import json
from pathlib import Path
import ssl
import sys
import urllib.error
import urllib.request

ORIGIN = "https://192.168.100.243:8443"
ENDPOINT = ORIGIN + "/gateway/liveops-prod/mcp"
TOKEN_A = "invalid-liveops-production-fixture-a"
TOKEN_B = "invalid-liveops-production-fixture-b"
NAMES = {"liveops_list_boards", "liveops_get_pallet_workspace", "liveops_list_pallet_sessions",
         "liveops_get_session_assortment", "liveops_list_replays"}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise RuntimeError("验收请求重定向被拒绝")


def make_request():
    context = ssl.create_default_context(cafile=str(Path.home() / ".config/unla243/tls/ca.crt"))
    opener = urllib.request.build_opener(NoRedirect(), urllib.request.HTTPSHandler(context=context))

    def request(url, body, extra, method="POST"):
        headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream", **extra}
        req = urllib.request.Request(url, data=json.dumps(body).encode() if body is not None else None, headers=headers, method=method)
        try:
            response = opener.open(req, timeout=35)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read(1024 * 1024 + 1)
            require(len(raw) <= 1024 * 1024, "验收响应超出预算")
            text = raw.decode()
            leaked = any(token in text or token in str(response.headers) for token in (TOKEN_A, TOKEN_B))
            try:
                if response.headers.get("Content-Type", "").startswith("text/event-stream"):
                    text = next(line[5:].strip() for line in text.splitlines() if line.startswith("data:"))
                data = json.loads(text) if text else {}
            except (ValueError, StopIteration):
                data = {}
            return response.status, data, dict(response.headers), leaked
    return request


def verify(request):
    initialize = {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-03-26", "capabilities": {}, "clientInfo": {"name": "production-rejection-check", "version": "1"}}}
    require(request(ENDPOINT, initialize, {})[0] == 401, "缺失认证未返回401")
    auth = {"Authorization": "Bearer " + TOKEN_A}
    status, data, headers, leaked = request(ENDPOINT, initialize, auth)
    session = next((v for k, v in headers.items() if k.lower() == "mcp-session-id"), None)
    try:
        require(session is not None, "初始化未返回会话ID")
        auth["Mcp-Session-Id"] = session
        auth["MCP-Protocol-Version"] = "2025-03-26"
        require(not leaked, "初始化响应回显fixture凭证")
        require(status == 200 and "result" in data, "初始化失败")
        status, _, _, leaked = request(ENDPOINT, {"jsonrpc": "2.0", "method": "notifications/initialized"}, auth)
        require(status in (200, 202, 204) and not leaked, "初始化通知失败或凭证回显")
        listing = {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}
        for url, credentials, expected in (
            (ENDPOINT, {"Mcp-Session-Id": session}, 401),
            (ENDPOINT, {**auth, "Authorization": "Bearer " + TOKEN_B}, 403),
            (ORIGIN + "/gateway/liveops-beta/mcp", auth, 403),
        ):
            status, _, _, leaked = request(url, listing, credentials)
            require(status == expected and not leaked, "缺失认证/换token/跨Beta前缀拒绝验收失败")
        status, data, _, leaked = request(ENDPOINT, listing, auth)
        tools = data.get("result", {}).get("tools", [])
        require(status == 200 and len(tools) == 5 and {t["name"] for t in tools} == NAMES and not leaked, "生产五工具目录不符")
        require(all("production" in t.get("description", "") and "beta" not in t.get("description", "").lower() for t in tools), "目录缺少production标识")
        call = {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "liveops_list_boards", "arguments": {"q": ""}}}
        status, data, _, leaked = request(ENDPOINT, call, auth)
        require(status == 200 and data.get("result", {}).get("isError") is True and not leaked, "无效上游凭证未返回isError或发生回显")
    finally:
        if session:
            status, _, _, leaked = request(ENDPOINT, None, {**auth, "Mcp-Session-Id": session}, method="DELETE")
            require(status in (200, 202, 204) and not leaked, "会话关闭失败；不得将405视为成功")
    print("通过：缺失401；换token/跨Beta前缀403；production五工具目录；无效上游凭证isError且不回显；会话已关闭")


if __name__ == "__main__":
    try:
        verify(make_request())
    except Exception as error:
        print("验收失败：" + (str(error) if isinstance(error, RuntimeError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
