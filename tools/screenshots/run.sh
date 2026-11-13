#!/bin/bash
# Runs inside the Playwright container (see `make screenshots`): starts nullops
# with demo users over HTTPS and captures the README screenshots.
set -e
cd /work
D=/work/data
rm -rf "$D"
./nullops -q --data-dir $D add-user -u admin -p 'Demo-Pass-2026'
./nullops -q --data-dir $D add-user -u ana -p 'Demo-Pass-2026'
./nullops -q --data-dir $D add-user -u carol -p 'Demo-Pass-2026' --mfa > /dev/null
TERM=dumb ./nullops --data-dir $D web -r /work/demo -p 8443 > /work/server.log 2>&1 &
for i in $(seq 1 50); do curl -sk -o /dev/null https://127.0.0.1:8443/ && break; sleep 0.2; done
pip install -q --disable-pip-version-check --root-user-action=ignore playwright==1.47.0
python3 /work/shoot.py
