"""配置 Beta 桥接的精确网络与身份委托目标，不重启服务。"""
import os
from pathlib import Path
import shutil

assert os.geteuid() == 0, 'Run with sudo'

def update(path: Path, values: dict[str, str]) -> None:
    original = path.read_text()
    backup = path.with_name(path.name + '.before-liveops-beta')
    if not backup.exists():
        shutil.copy2(path, backup)
        os.chmod(backup, 0o600)
    lines = [line for line in original.splitlines() if not any(line.startswith(key + '=') for key in values)]
    path.write_text('\n'.join(lines + [key + '=' + value for key, value in values.items()]) + '\n')

gateway = Path('/home/ubuntu/.config/unla243/runtime.env')
current = dict(line.split('=', 1) for line in gateway.read_text().splitlines() if '=' in line and not line.startswith('#'))
allowed = current.get('INTERNAL_NETWORK_ALLOWLIST', '').strip('"\'').split(',')
if '100.92.212.67' not in allowed:
    allowed.append('100.92.212.67')
update(gateway, {
    'INTERNAL_NETWORK_ALLOWLIST': ','.join(value for value in allowed if value),
    'LOGGER_LEVEL': 'info',
    'TRACING_ENABLED': 'false',
    'TRACING_ERR_BODY_ENABLED': 'false',
    'TRACING_REQ_ARGS_ENABLED': 'false',
    'TRACING_REQ_BODY_ENABLED': 'false',
    'TRACING_RESP_BODY_ENABLED': 'false',
})
update(Path('/home/ubuntu/workspace/exai/.local/runtime.env'), {
    'EXAI_MCP_USER_AUTH_ENDPOINT': 'https://192.168.100.243:8443/gateway/liveops-beta/mcp',
})
print('Beta bridge target configured; service restart is handled by the release workflow')
