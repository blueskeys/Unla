"""验证业务桥接拒绝路径；仅使用无效测试凭证，不读取业务数据。"""
import json
from pathlib import Path
import ssl
import urllib.error
import urllib.request
import yaml

origin = 'https://192.168.100.243:8443'
endpoint = origin + '/gateway/liveops-beta/mcp'
context = ssl.create_default_context(cafile=str(Path.home()/'.config/unla243/tls/ca.crt'))
headers = {'Content-Type': 'application/json', 'Accept': 'application/json, text/event-stream'}

def request(url: str, body: dict, extra: dict[str, str]) -> tuple[int, dict, dict]:
    req = urllib.request.Request(url, data=json.dumps(body).encode(), headers={**headers, **extra})
    try:
        response = urllib.request.urlopen(req, context=context, timeout=35)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read().decode()
        if raw.startswith('event:') or raw.startswith('data:'):
            raw = next(line[6:] for line in raw.splitlines() if line.startswith('data: '))
        return response.status, json.loads(raw) if raw else {}, dict(response.headers)

initialize = {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-03-26', 'capabilities': {}, 'clientInfo': {'name': 'beta-rejection-check', 'version': '1'}}}
assert request(endpoint, initialize, {})[0] == 401
auth = {'Authorization': 'Bearer invalid-liveops-fixture-a'}
status, data, response_headers = request(endpoint, initialize, auth)
assert status == 200 and 'result' in data, (status, data)
session = next(value for key, value in response_headers.items() if key.lower() == 'mcp-session-id')
auth['Mcp-Session-Id'] = session
listing = {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list', 'params': {}}
assert request(endpoint, listing, {'Mcp-Session-Id': session})[0] == 401
assert request(endpoint, listing, {**auth, 'Authorization': 'Bearer invalid-liveops-fixture-b'})[0] == 403
assert request(origin + '/gateway/exai-test/mcp', listing, auth)[0] == 403
status, data, _ = request(endpoint, listing, auth)
expected = yaml.safe_load(Path(__file__).with_name('liveops-beta.yaml').read_text())
assert status == 200 and {tool['name'] for tool in data['result']['tools']} == {tool['name'] for tool in expected['tools']}
status, data, _ = request(endpoint, {'jsonrpc': '2.0', 'id': 3, 'method': 'tools/call', 'params': {'name': 'liveops_list_boards', 'arguments': {'q': ''}}}, auth)
assert status == 200 and data.get('result', {}).get('isError') is True, (status, data)
assert 'invalid-liveops-fixture-a' not in json.dumps(data)
try:
    req = urllib.request.Request(endpoint, method='DELETE', headers={**headers, **auth})
    urllib.request.urlopen(req, context=context, timeout=10).close()
except urllib.error.HTTPError as error:
    assert error.code in (200, 202, 204, 405)
print('PASS: missing bearer 401; changed bearer/cross-prefix 403; invalid upstream bearer isError; approved read-only directory matches')
