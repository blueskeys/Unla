"""Create a private test CA and an IP-bound server certificate."""
import os
from pathlib import Path
import subprocess

root = Path.home()/'.config/unla243'
tls = root/'tls'
tls.mkdir(mode=0o700, exist_ok=True)
os.umask(0o077)
def run(*args):
    subprocess.run(['openssl', *args], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
if not (tls/'ca.crt').exists():
    run('req', '-x509', '-newkey', 'rsa:3072', '-nodes', '-days', '365', '-keyout', str(tls/'ca.key'), '-out', str(tls/'ca.crt'), '-subj', '/CN=Unla243 Internal Test CA', '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign')
if not (tls/'server.crt').exists():
    run('req', '-new', '-newkey', 'rsa:2048', '-nodes', '-keyout', str(tls/'server.key'), '-out', str(tls/'server.csr'), '-subj', '/CN=192.168.100.243')
    (tls/'extensions.cnf').write_text('subjectAltName=IP:192.168.100.243\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n')
    run('x509', '-req', '-in', str(tls/'server.csr'), '-CA', str(tls/'ca.crt'), '-CAkey', str(tls/'ca.key'), '-CAcreateserial', '-out', str(tls/'server.crt'), '-days', '90', '-extfile', str(tls/'extensions.cnf'))
settings = root/'runtime.env'
lines = settings.read_text().splitlines()
updates = {'INTERNAL_NETWORK_ALLOWLIST':'web', 'FORWARD_ENABLED':'true', 'FORWARD_MCP_ARG_KEY_FOR_HEADER':'', 'FORWARD_ALLOW_HEADERS':'accept', 'GIN_MODE':'release'}
for key,value in updates.items():
    lines = [line for line in lines if not line.startswith(key+'=')]
    lines.append(key+'='+value)
settings.write_text('\n'.join(lines)+'\n')
os.chmod(settings, 0o600)
print('TLS and test-only gateway settings prepared; no secrets printed')
