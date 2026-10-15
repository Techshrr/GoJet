#!/usr/bin/env bash
set -euo pipefail
# Test-only tooling. Do not mutate product dependencies or weaken the page CSP.
audit_dir="${RUNNER_TEMP:?}/gojet-p20-axe"
npm install --prefix "$audit_dir" --no-save --package-lock=false --ignore-scripts --no-audit --no-fund axe-core@4.10.3
node -e 'const p=require(process.argv[1]); if(p.version!=="4.10.3") throw Error("axe version mismatch")' "$audit_dir/node_modules/axe-core/package.json"
printf 'P20_AXE_SOURCE=%s/node_modules/axe-core/axe.min.js\n' "$audit_dir" >> "${GITHUB_ENV:?}"
node --test scripts/p20/test_t035_accessibility_probe.mjs
