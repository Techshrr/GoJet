#!/usr/bin/env bash
set -euo pipefail
if [[ "${P20_ORCA_SESSION:-}" != 1 ]]; then
  exec xvfb-run -a -s '-screen 0 1600x1000x24' dbus-run-session -- env P20_ORCA_SESSION=1 bash "$0" "$@"
fi
export NO_AT_BRIDGE=0
export GTK_MODULES=atk-bridge
export LANG=C.UTF-8
export P20_ORCA_LOG="${RUNNER_TEMP:?}/gojet-orca-$$.log"
# Keep full desktop debug logs out of artifacts: they can include fixture
# values. The probe retains only relevant control announcements.
openbox > "${RUNNER_TEMP}/gojet-openbox-$$.log" 2>&1 &
wm_pid=$!
speech-dispatcher --spawn
orca --replace --enable=speech --disable=braille --debug-file="$P20_ORCA_LOG" > "${RUNNER_TEMP}/gojet-orca-stderr-$$.log" 2>&1 &
orca_pid=$!
export P20_ORCA_PID="$orca_pid"
export P20_ORCA_VERSION="$(orca --version)"
trap 'kill "$orca_pid" "$wm_pid" 2>/dev/null || true' EXIT
for attempt in $(seq 1 30); do
  kill -0 "$orca_pid"
  if [[ -s "$P20_ORCA_LOG" ]]; then break; fi
  sleep 1
done
test -s "$P20_ORCA_LOG"
"$@"
