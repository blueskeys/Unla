"""通过 Unla 管理 API 注册固定 Beta 只读工具，不输出登录秘密。"""
import json
from pathlib import Path
import urllib.request

base = 'http://192.168.100.243:8080'
env = dict(line.split('=', 1) for line in (Path.home()/'.config/unla243/runtime.env').read_text().splitlines() if '=' in line)

def request(path: str, method: str, body: bytes | None, headers: dict[str, str]) -> dict:
    req = urllib.request.Request(base + path, data=body, method=method, headers=headers)
    with urllib.request.urlopen(req, timeout=15) as response:
        data = response.read()
        return json.loads(data) if data else {}

login = request('/api/auth/login', 'POST', json.dumps({'username': env['SUPER_ADMIN_USERNAME'], 'password': env['SUPER_ADMIN_PASSWORD']}).encode(), {'Content-Type': 'application/json'})
headers = {'Authorization': 'Bearer ' + login['token'], 'Content-Type': 'application/yaml'}
configs = request('/api/mcp/configs', 'GET', None, headers)['data']
assert not any(item.get('name') == 'liveops-beta' for item in configs), 'Configuration exists; inspect before updating'
request('/api/mcp/configs', 'POST', Path(__file__).with_name('liveops-beta.yaml').read_bytes(), headers)
print('Registered default/liveops-beta with one read-only tool')
