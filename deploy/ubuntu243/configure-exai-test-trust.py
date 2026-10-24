"""Trust only this test CA and explicit origin in the two ExAI services."""
import os
from pathlib import Path
import shutil
import subprocess

assert os.geteuid() == 0, 'Run with sudo'
origin = 'https://192.168.100.243:8443'
source = Path('/home/ubuntu/.config/unla243/tls/ca.crt').read_bytes()
active = subprocess.check_output(['docker','exec','exai-ubuntu243-postgres','psql','-X','-U','exai_admin','-d','exai','-Atqc',"select count(*) from agent_runs where status in ('queued','running')"], text=True).strip()
assert active == '0', 'Active ExAI runs exist; postpone configuration restart'
trust = Path('/etc/exai/unla243')
trust.mkdir(parents=True, exist_ok=True, mode=0o755)
changed = False
for role in ('runtime', 'core'):
    target = Path('/home/ubuntu/workspace/exai/.local')/(role+'.env')
    original = target.read_text()
    values = dict(line.split('=',1) for line in original.splitlines() if '=' in line and not line.startswith('#'))
    cert = trust/(role+'-ca.pem')
    previous = values.get('NODE_EXTRA_CA_CERTS','').strip().strip('"').strip("'")
    bundle = Path(previous).read_bytes() if previous else b''
    if source not in bundle:
        bundle += b'\n'+source
    cert.write_bytes(bundle)
    os.chmod(cert, 0o644)
    origins = [value for value in values.get('EXAI_MCP_PRIVATE_ALLOWED_ORIGINS','').strip().strip('"').strip("'").split(',') if value]
    if origin not in origins:
        origins.append(origin)
    updates = {'EXAI_MCP_PRIVATE_ALLOWED_ORIGINS':','.join(origins),'NODE_EXTRA_CA_CERTS':str(cert)}
    lines = [line for line in original.splitlines() if not any(line.startswith(key+'=') for key in updates)]
    result = '\n'.join(lines+[key+'='+value for key,value in updates.items()])+'\n'
    if result != original:
        backup = target.with_name(target.name+'.before-unla-test')
        if not backup.exists():
            shutil.copy2(target, backup)
            os.chmod(backup, 0o600)
        target.write_text(result)
        changed = True
if changed:
    subprocess.run(['systemctl','restart','exai-ubuntu243-runtime.service','exai-ubuntu243-core.service'], check=True)
print('ExAI explicit MCP origin and CA configured; restarted only if changed')
