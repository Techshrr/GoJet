#!/usr/bin/env python3
from pathlib import Path
import sys

repo = Path(sys.argv[1]).resolve()
source = repo / '.github/workflows/p20-p0-bio-formal.yml'
target = repo / '.github/workflows/p20-p0-domain.yml'
text = source.read_text(encoding='utf-8')

def replace_once(old: str, new: str) -> None:
    global text
    count = text.count(old)
    assert count == 1, (old, count)
    text = text.replace(old, new, 1)

replace_once('name: P20 P0 Bio Formal Timeline', 'name: P20 P0 Domain Timeline')
replace_once("      - 'scripts/p20/t018_probe/**'\n", "      - 'scripts/p20/t018_probe/**'\n      - 'scripts/p20/t019_probe/**'\n")
replace_once("      - 'scripts/p11/**'\n", "      - 'scripts/p11/**'\n      - 'scripts/p06/**'\n      - 'scripts/p17/integration.py'\n      - 'scripts/p17/domain_case_runner/**'\n")
replace_once("      - '.github/workflows/p20-p0-bio-formal.yml'\n", "      - '.github/workflows/p20-p0-bio-formal.yml'\n      - '.github/workflows/p20-p0-domain.yml'\n")
replace_once('group: p20-p0-bio-formal-${{ github.event.pull_request.number || github.ref }}', 'group: p20-p0-domain-${{ github.event.pull_request.number || github.ref }}')
replace_once('  t009-t018-formal:\n    name: P20-T009/T018 real Bio formal timeline', '  t009-t019-live:\n    name: P20-T009/T019 real custom-domain timeline')
replace_once("      GOJET_P20_T018_PROBE_OUT: artifacts/v10/P20/runtime/t018/probe.json\n", "      GOJET_P20_T018_PROBE_OUT: artifacts/v10/P20/runtime/t018/probe.json\n      GOJET_P20_T019_PROBE_OUT: artifacts/v10/P20/runtime/t019/probe.json\n")
replace_once('      - name: Verify formal T018 source and frozen contract boundary', '      - name: Verify T019 source and frozen contract boundary')
replace_once('scripts/p20/t018_case.py scripts/p20/validate_contract.py', 'scripts/p20/t018_case.py scripts/p20/validate_contract.py scripts/p06/integration.py scripts/p17/integration.py')
replace_once('scripts/p20/t018_probe/main.go | tee /tmp/p20-t018-formal-gofmt.diff', 'scripts/p20/t018_probe/main.go scripts/p20/t019_probe/main.go | tee /tmp/p20-t019-gofmt.diff')
replace_once('test ! -s /tmp/p20-t018-formal-gofmt.diff', 'test ! -s /tmp/p20-t019-gofmt.diff')
replace_once('./scripts/p20/t018_probe\n', './scripts/p20/t018_probe ./scripts/p20/t019_probe\n')
replace_once('artifacts/v10/P20/runtime/t017 artifacts/v10/P20/runtime/t018\n', 'artifacts/v10/P20/runtime/t017 artifacts/v10/P20/runtime/t018 artifacts/v10/P20/runtime/t019\n')
replace_once('          sha256sum /tmp/gojet-p20-platformapi > artifacts/v10/P20/runtime/t018/native-platformapi.sha256\n', '          sha256sum /tmp/gojet-p20-platformapi > artifacts/v10/P20/runtime/t018/native-platformapi.sha256\n          sha256sum /tmp/gojet-p20-platformapi > artifacts/v10/P20/runtime/t019/native-platformapi.sha256\n')

anchor = '      - name: Start exact-head native P20 services\n'
assert text.count(anchor) == 1
predecessor = """      - name: Prove exact-head P06 and P17 custom-domain predecessor authority
        shell: bash
        run: |
          set -euo pipefail
          exact_head=\"$(git rev-parse HEAD)\"
          mkdir -p artifacts/v10/P06/results artifacts/v10/P17/domain artifacts/v10/P20/runtime/t019

          for case in P06-T002 P06-T009 P06-T010 P06-T011 P06-T012 P06-T013 P06-T014 P06-T018; do
            echo \"Running ${case} as T019 custom-domain predecessor authority\"
            GITHUB_SHA=\"$exact_head\" python3 scripts/p06/integration.py --case \"$case\"
          done

          export MYSQL_PWD=root
          for n in 8 9; do
            case_id=\"$(printf 'P17-T%03d' \"$n\")\"
            echo \"Preparing pristine durable authority for ${case_id}\"
            mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root -e \"DROP DATABASE IF EXISTS gojet_test; CREATE DATABASE gojet_test CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;\"
            for migration in migrations/[0-9][0-9][0-9][0-9][0-9][0-9]_*.sql; do
              mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root --default-character-set=utf8mb4 gojet_test < \"$migration\"
            done
            redis-cli -h 127.0.0.1 -p 6379 FLUSHDB >/dev/null
            GITHUB_SHA=\"$exact_head\" python3 scripts/p17/integration.py --case \"$case_id\"
          done

          EXACT_HEAD=\"$exact_head\" python3 - <<'PRED'
          import json, os
          from pathlib import Path
          head=os.environ['EXACT_HEAD']
          p06=['P06-T002','P06-T009','P06-T010','P06-T011','P06-T012','P06-T013','P06-T014','P06-T018']
          for case in p06:
              data=json.loads((Path('artifacts/v10/P06/results')/f'{case}.json').read_text(encoding='utf-8'))
              assert data['status']=='PASS',data
              assert data['implementation_commit']==head,data
              assert data['errors']==[],data
          for case in ('P17-T008','P17-T009'):
              data=json.loads((Path('artifacts/v10/P17/domain')/f'{case}.json').read_text(encoding='utf-8'))
              assert data['status']=='PASS',data
              assert data['exact_head']==head,data
              assert data['errors']==[],data
          PRED

          echo 'Resetting durable state for the correlated P20 timeline'
          mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root -e \"DROP DATABASE IF EXISTS gojet_test; CREATE DATABASE gojet_test CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;\"
          for migration in migrations/[0-9][0-9][0-9][0-9][0-9][0-9]_*.sql; do
            mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root --default-character-set=utf8mb4 gojet_test < \"$migration\"
          done
          redis-cli -h 127.0.0.1 -p 6379 FLUSHDB >/dev/null
          rm -rf \"$GOJET_FILE_STORAGE_ROOT\"
          mkdir -p \"$GOJET_FILE_STORAGE_ROOT\"

"""
text = text.replace(anchor, predecessor + anchor, 1)

capture_anchor = '      - name: Capture T018 authoritative runtime state\n'
assert text.count(capture_anchor) == 1
t019_step = """      - name: Run exact-head P20-T019 custom-domain live probe
        shell: bash
        run: |
          set -euo pipefail
          exact_head=\"$(git rev-parse HEAD)\"
          P20_EXACT_HEAD=\"$exact_head\" go run ./scripts/p20/t019_probe

"""
text = text.replace(capture_anchor, t019_step + '      - name: Capture T019 authoritative runtime state\n', 1)

log_anchor = '          cp artifacts/v10/P20/runtime/t009/platformapi.log artifacts/v10/P20/runtime/t018/platformapi.log 2>/dev/null || true\n'
assert text.count(log_anchor) == 1
capture_extra = """          cp artifacts/v10/P20/runtime/t009/platformapi.log artifacts/v10/P20/runtime/t019/platformapi.log 2>/dev/null || true
          mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root -N -B gojet_test -e \"SELECT workspace_id,source,source_key,status,domain_limit,starts_at,expires_at,support_ticket_id FROM custom_domain_entitlement_sources ORDER BY id\" > artifacts/v10/P20/runtime/t019/mysql-entitlements.tsv 2> artifacts/v10/P20/runtime/t019/mysql-entitlements.err
          mysql --protocol=tcp -h 127.0.0.1 -P 3306 -u root -N -B gojet_test -e \"SELECT id,workspace_id,hostname_ascii,routing_state,ownership_status,ingress_dns_status,https_status,risk_status FROM custom_domains ORDER BY id\" > artifacts/v10/P20/runtime/t019/mysql-domains.tsv 2> artifacts/v10/P20/runtime/t019/mysql-domains.err
"""
text = text.replace(log_anchor, log_anchor + capture_extra, 1)

replace_once('      - name: Upload exact-head T018 formal evidence', '      - name: Upload exact-head T019 live evidence')
replace_once('name: p20-t018-formal-bio-${{ github.event.pull_request.head.sha || github.sha }}', 'name: p20-t019-live-domain-${{ github.event.pull_request.head.sha || github.sha }}')
upload_anchor = '            artifacts/v10/P20/p0/P20-T018.json\n'
assert text.count(upload_anchor) == 1
predecessor_upload = """            artifacts/v10/P06/results/P06-T002.json
            artifacts/v10/P06/results/P06-T009.json
            artifacts/v10/P06/results/P06-T010.json
            artifacts/v10/P06/results/P06-T011.json
            artifacts/v10/P06/results/P06-T012.json
            artifacts/v10/P06/results/P06-T013.json
            artifacts/v10/P06/results/P06-T014.json
            artifacts/v10/P06/results/P06-T018.json
            artifacts/v10/P17/domain/P17-T008.json
            artifacts/v10/P17/domain/P17-T009.json
"""
text = text.replace(upload_anchor, predecessor_upload + upload_anchor, 1)
replace_once('            artifacts/v10/P20/runtime/t018/\n', '            artifacts/v10/P20/runtime/t018/\n            artifacts/v10/P20/runtime/t019/\n')

target.write_text(text, encoding='utf-8')
