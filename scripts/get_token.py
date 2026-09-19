import hmac
import hashlib
import time
import os
import urllib.request
import json
import sys

server_ip = sys.argv[1] if len(sys.argv) > 1 else os.environ.get('WARLINK_SERVER_IP', '')
if not server_ip:
    server_api = os.environ.get('WARLINK_SERVER_API', '')
    if server_api:
        server_ip = server_api.replace('http://', '').replace('https://', '').split('/')[0]

if not server_ip:
    print(json.dumps({'error': 'WARLINK_SERVER_IP environment variable is required'}))
    sys.exit(1)

device_id = os.environ.get('WARLINK_DEVICE_ID', 'capture_dev')
ts = int(time.time())
nonce = os.urandom(8).hex()

headers = {'Content-Type': 'application/json'}
hmac_secret = os.environ.get('WARLINK_HMAC_SECRET', '')
if hmac_secret:
    data_to_sign = f'{device_id}:{ts}:{nonce}'.encode()
    sig = hmac.new(hmac_secret.encode(), data_to_sign, hashlib.sha256).hexdigest()
    headers['X-Signature'] = sig

try:
    url = f'http://{server_ip}/api/v1/session'
    payload = json.dumps({'device_id': device_id, 'timestamp': ts, 'nonce': nonce}).encode()
    req = urllib.request.Request(url, data=payload, headers=headers)
    with urllib.request.urlopen(req, timeout=5) as resp:
        data = json.loads(resp.read().decode())
        print(json.dumps(data))
except Exception as e:
    print(json.dumps({'error': str(e)}))
