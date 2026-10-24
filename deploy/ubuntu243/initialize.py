"""Initialize private deployment settings without printing credentials."""
import os
from pathlib import Path
import secrets

root = Path.home() / '.config' / 'unla243'
root.mkdir(parents=True, exist_ok=True, mode=0o700)
os.chmod(root, 0o700)
settings = root / 'runtime.env'
values = {
    'SUPER_ADMIN_USERNAME': 'admin',
    'SUPER_ADMIN_PASSWORD': secrets.token_urlsafe(24),
    'APISERVER_JWT_SECRET_KEY': secrets.token_hex(32),
    'APISERVER_DB_NAME': '/app/data/users.db',
    'GATEWAY_DB_NAME': '/app/data/gateway.db',
    'GATEWAY_STORAGE_TYPE': 'db',
    'LOGGER_LEVEL': 'info',
    'APISERVER_LOGGER_LEVEL': 'info',
    'APISERVER_LOGGER_COLOR': 'false',
    'TRACING_ENABLED': 'false',
    'MCP_GATEWAY_RELOAD_INTERVAL': '30s',
    'INTERNAL_NETWORK_VALIDATION_ENABLED': 'true',
    'INTERNAL_NETWORK_ALLOWLIST': '',
    'VITE_GATEWAY_SERVICE_BASE_URL': 'http://192.168.100.243:5235',
    'TZ': 'Asia/Shanghai',
}
if not settings.exists():
    with settings.open('x') as target:
        os.chmod(settings, 0o600)
        target.write(''.join(f'{key}={value}\n' for key, value in values.items()))
print(f'Private settings ready: {settings}')
