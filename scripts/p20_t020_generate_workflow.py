#!/usr/bin/env python3
from pathlib import Path

source = Path('.github/workflows/p20-p0-domain.yml')
text = source.read_text(encoding='utf-8')


def once(old: str, new: str) -> None:
    global text
    n = text.count(old)
    assert n == 1, (old, n)
    text = text.replace(old, new, 1)


once('name: P20 P0 Domain Timeline', 'name: P20 P0 Support Timeline')
once("      - 'scripts/p20/t019_formal_probe/**'\n", "      - 'scripts/p20/t019_formal_probe/**'\n      - 'scripts/p20/t020_probe/**'\n")
once("      - 'scripts/p17/domain_case_runner/**'\n", "      - 'scripts/p17/domain_case_runner/**'\n      - 'scripts/p14/**'\n")
once("      - '.github/workflows/p20-p0-domain.yml'\n", "      - '.github/workflows/p20-p0-domain.yml'\n      - '.github/workflows/p20-p0-support.yml'\n      - '.github/workflows/p14-integration.yml'\n")
once('group: p20-p0-domain-${{ github.event.pull_request.number || github.ref }}', 'group: p20-p0-support-${{ github.event.pull_request.number || github.ref }}')
once('  t009-t019-formal:\n    name: P20-T009/T019 real custom-domain formal timeline', '  t009-t020-live:\n    name: P20-T009/T020 real Support live timeline')
once('    timeout-minutes: 100', '    timeout-minutes: 130')

once(
    "      GOJET_BIO_WORKSPACE_QUOTA: '20'\n",
    """      GOJET_BIO_WORKSPACE_QUOTA: '20'
      GOJET_SUPPORT_ENABLED: '1'
      GOJET_TURNSTILE_SECRET: p20-t020-ci-placeholder-not-a-production-secret
      GOJET_SUPPORT_RATE_LIMIT: '20'
      GOJET_SUPPORT_RATE_WINDOW: 5m
      GOJET_SUPPORT_TURNSTILE_REPLAY_TTL: 10m
      GOJET_TEST_SUPPORT_TURNSTILE_ENABLED: '1'
      GOJET_TEST_SUPPORT_TURNSTILE_TOKEN: p20-t020-deterministic-turnstile
      GOJET_TEST_SUPPORT_TICKETS_ADMIN_ENABLED: '1'
      GOJET_TEST_SUPPORT_TICKETS_ADMIN_ACTOR: p14-ticket-admin
      GOJET_TEST_SUPPORT_MAIL_ADMIN_ENABLED: '1'
      GOJET_TEST_SUPPORT_MAIL_ADMIN_ACTOR: p14-mail-admin
      GOJET_TEST_P14_PRODUCER: /tmp/gojet-p20-p14-producer
      GOJET_TEST_P14_MAILWORKER: /tmp/gojet-p20-mailworker
      GOJET_TEST_P14_ATTACHMENT_ROOT: /tmp/gojet-p20/p14-attachments
      GOJET_TEST_P14_ATTACHMENT_POLICY: txt=text/plain
      GOJET_TEST_P14_ATTACHMENT_MAX_BYTES: '65536'
      GOJET_TEST_P14_CLAMAV_NETWORK: tcp
      GOJET_TEST_P14_CLAMAV_ADDRESS: 127.0.0.1:3310
      GOJET_TEST_P14_CLAMAV_DIAL_TIMEOUT: 2s
      GOJET_TEST_P14_CLAMAV_SCAN_TIMEOUT: 5s
      GOJET_TEST_P14_CLAMAV_MAX_SIGNATURE_AGE: 48h
      GOJET_TEST_P14_SMTP_STATE: /tmp/gojet-p20/p14-smtp-state.json
      GOJET_TEST_P14_SMTP_MODE: /tmp/gojet-p20/p14-smtp-mode
      GOJET_TEST_P14_TMP: /tmp/gojet-p20
      GOJET_SMTP_ADDR: 127.0.0.1:2525
      GOJET_SMTP_FROM: no-reply@gojet.local
      GOJET_MAILWORKER_INTERVAL: 250ms
""",
)
once('      GOJET_P20_T019_PROBE_OUT: artifacts/v10/P20/runtime/t019/probe.json\n', '      GOJET_P20_T019_PROBE_OUT: artifacts/v10/P20/runtime/t019/probe.json\n      GOJET_P20_T020_PROBE_OUT: artifacts/v10/P20/runtime/t020/probe.json\n')

once('scripts/p20/t019_formal_probe/main.go | tee /tmp/p20-t019-gofmt.diff', 'scripts/p20/t019_formal_probe/main.go scripts/p20/t020_probe/main.go | tee /tmp/p20-t020-gofmt.diff')
once('test ! -s /tmp/p20-t019-gofmt.diff', 'test ! -s /tmp/p20-t020-gofmt.diff')
once('./scripts/p20/t019_formal_probe\n', './scripts/p20/t019_formal_probe ./scripts/p20/t020_probe\n')

once(
    'mkdir -p /tmp/gojet-p20 artifacts/v10/P20/runtime/t016 artifacts/v10/P20/runtime/t017 artifacts/v10/P20/runtime/t018 artifacts/v10/P09/clamav artifacts/v10/P11/api artifacts/v10/P11/headers artifacts/v10/P11/sitemap',
    'mkdir -p /tmp/gojet-p20 /tmp/gojet-p20/p14-attachments artifacts/v10/P20/runtime/t016 artifacts/v10/P20/runtime/t017 artifacts/v10/P20/runtime/t018 artifacts/v10/P20/runtime/t020 artifacts/v10/P09/clamav artifacts/v10/P11/api artifacts/v10/P11/headers artifacts/v10/P11/sitemap artifacts/v10/P14/runtime artifacts/v10/P14/results',
)

once(
    '          go build -trimpath -o /tmp/gojet-p20-analyticsreconciler ./services/analyticsreconciler/cmd/reconciler\n',
    '''          go build -trimpath -o /tmp/gojet-p20-analyticsreconciler ./services/analyticsreconciler/cmd/reconciler
          go build -trimpath -o /tmp/gojet-p20-mailworker ./services/platformapi/cmd/mailworker
          go build -trimpath -o /tmp/gojet-p20-p14-producer ./scripts/p14/producer.go
''',
)

anchor = '      - name: Prove exact-head P06 and P17 custom-domain predecessor authority\n'
assert text.count(anchor) == 1
p14 = r'''      - name: Prove exact-head P14 Support Mail attachment predecessor authority
        shell: bash
        run: |
          set -euo pipefail
          exact_head="$(git rev-parse HEAD)"
          mkdir -p artifacts/v10/P14/api artifacts/v10/P14/rbac artifacts/v10/P14/security artifacts/v10/P14/entitlement artifacts/v10/P14/mail artifacts/v10/P14/notification artifacts/v10/P14/audit artifacts/v10/P14/runtime artifacts/v10/P14/results /tmp/gojet-p20/p14-attachments
          printf '%s\n' success > "$GOJET_TEST_P14_SMTP_MODE"
          python3 scripts/p14/smtp_sink.py --host 127.0.0.1 --port 2525 --state "$GOJET_TEST_P14_SMTP_STATE" --mode-file "$GOJET_TEST_P14_SMTP_MODE" > /tmp/gojet-p20/p14-smtp.log 2>&1 &
          echo $! > /tmp/gojet-p20/p14-smtp.pid
          python3 - <<'READY'
          import socket,time
          deadline=time.monotonic()+15
          while time.monotonic()<deadline:
              try:
                  with socket.create_connection(('127.0.0.1',2525),timeout=1) as s:
                      if s.recv(1024).startswith(b'220 '):
                          raise SystemExit(0)
              except OSError:
                  time.sleep(.25)
          raise SystemExit('P14 SMTP sink not ready')
          READY
          GOJET_PLATFORMAPI_ADDR=127.0.0.1:18082 GOJET_TEST_AUTH_ENABLED=1 /tmp/gojet-p20-platformapi > /tmp/gojet-p20/p14-platformapi.log 2>&1 &
          echo $! > /tmp/gojet-p20/p14-platformapi.pid
          python3 - <<'READY'
          import http.client,time
          deadline=time.monotonic()+20
          last=None
          while time.monotonic()<deadline:
              try:
                  c=http.client.HTTPConnection('127.0.0.1',18082,timeout=1)
                  c.request('GET','/api/support/tickets?workspace_id=probe')
                  r=c.getresponse(); r.read(); status=r.status; c.close()
                  if status==401:
                      raise SystemExit(0)
                  last=status
              except Exception as exc:
                  last=exc
              time.sleep(.25)
          raise SystemExit(f'P14 test platformapi not ready: {last}')
          READY
          for index in $(seq 1 21); do
            case_id="$(printf 'P14-T%03d' "$index")"
            echo "Running ${case_id} as T020 predecessor authority"
            GITHUB_SHA="$exact_head" python3 scripts/p14/integration.py --case "$case_id"
          done
          P14_HEAD="$exact_head" python3 - <<'VERIFY'
          import json,os
          from pathlib import Path
          root=Path('artifacts/v10/P14')
          mapping={1:'api',2:'rbac',3:'api',4:'security',5:'entitlement',6:'entitlement',7:'entitlement',8:'security',9:'security',10:'security',11:'security',12:'security',13:'api',14:'mail',15:'mail',16:'mail',17:'mail',18:'notification',19:'rbac',20:'api',21:'audit'}
          head=os.environ['P14_HEAD']; cases=[]
          for i in range(1,22):
              cid=f'P14-T{i:03d}'
              d=json.loads((root/mapping[i]/f'{cid}.json').read_text(encoding='utf-8'))
              assert d['case_id']==cid and d['status']=='PASS' and d['implementation_commit']==head and d['errors']==[],d
              cases.append({'case_id':cid,'status':'PASS','implementation_commit':head})
          (root/'results'/'integration-summary.json').write_text(json.dumps({'node':'P14','implementation_commit':head,'case_range':'P14-T001..P14-T021','status':'PASS','cases':cases,'errors':[]},indent=2,sort_keys=True)+'\n',encoding='utf-8')
          VERIFY
          kill "$(cat /tmp/gojet-p20/p14-platformapi.pid)" 2>/dev/null || true
          wait "$(cat /tmp/gojet-p20/p14-platformapi.pid)" 2>/dev/null || true
          kill "$(cat /tmp/gojet-p20/p14-smtp.pid)" 2>/dev/null || true
          wait "$(cat /tmp/gojet-p20/p14-smtp.pid)" 2>/dev/null || true

'''
text = text.replace(anchor, p14 + anchor, 1)

formal = '      - name: Capture T019 authoritative runtime state\n'
assert text.count(formal) == 1
t020 = r'''      - name: Run exact-head P20-T020 Support discovery probe
        shell: bash
        run: |
          set -euo pipefail
          exact_head="$(git rev-parse HEAD)"
          P20_EXACT_HEAD="$exact_head" go run ./scripts/p20/t020_probe

'''
text = text.replace(formal, t020 + '      - name: Capture T020 authoritative runtime state\n', 1)

once('          mkdir -p artifacts/v10/P20/runtime/t018 artifacts/v10/P20/runtime/t019\n', '          mkdir -p artifacts/v10/P20/runtime/t018 artifacts/v10/P20/runtime/t019 artifacts/v10/P20/runtime/t020\n')
capture = '''          mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root -N -B gojet_test -e "SELECT id,workspace_id,hostname_ascii,routing_state,ownership_status,ingress_dns_status,https_status,risk_status FROM custom_domains ORDER BY id" > artifacts/v10/P20/runtime/t019/mysql-domains.tsv 2> artifacts/v10/P20/runtime/t019/mysql-domains.err
'''
assert text.count(capture) == 1
extra = '''          cp artifacts/v10/P20/runtime/t009/platformapi.log artifacts/v10/P20/runtime/t020/platformapi.log 2>/dev/null || true
          mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root -N -B gojet_test -e "SELECT id,workspace_id,requester_user_id,category,status,version,correlation_id FROM support_tickets ORDER BY created_at,id" > artifacts/v10/P20/runtime/t020/mysql-support-tickets.tsv 2> artifacts/v10/P20/runtime/t020/mysql-support-tickets.err
          mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root -N -B gojet_test -e "SELECT id,ticket_id,actor_type,actor_id,kind,correlation_id FROM support_ticket_messages ORDER BY created_at,id" > artifacts/v10/P20/runtime/t020/mysql-support-messages.tsv 2> artifacts/v10/P20/runtime/t020/mysql-support-messages.err
          mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root -N -B gojet_test -e "SELECT id,template_key,resource_type,resource_id,status,attempt_count,last_error_code FROM mail_jobs ORDER BY created_at,id" > artifacts/v10/P20/runtime/t020/mysql-mail-jobs.tsv 2> artifacts/v10/P20/runtime/t020/mysql-mail-jobs.err
'''
text = text.replace(capture, capture + extra, 1)

once('      - name: Upload exact-head T019 formal evidence', '      - name: Upload exact-head T020 live evidence')
once('name: p20-t019-formal-domain-${{ github.event.pull_request.head.sha || github.sha }}', 'name: p20-t020-live-support-${{ github.event.pull_request.head.sha || github.sha }}')
once(
    '            artifacts/v10/P20/p0/P20-T019.json\n',
    '''            artifacts/v10/P20/p0/P20-T019.json
            artifacts/v10/P14/api/
            artifacts/v10/P14/rbac/
            artifacts/v10/P14/security/
            artifacts/v10/P14/entitlement/
            artifacts/v10/P14/mail/
            artifacts/v10/P14/notification/
            artifacts/v10/P14/audit/
            artifacts/v10/P14/results/integration-summary.json
''',
)
once('            artifacts/v10/P20/runtime/t019/\n', '            artifacts/v10/P20/runtime/t019/\n            artifacts/v10/P20/runtime/t020/\n')

out = Path('staging/p20-p0-support.yml.generated')
out.parent.mkdir(parents=True, exist_ok=True)
out.write_text(text, encoding='utf-8')
