#!/usr/bin/env bash
set -euo pipefail
if [[ "${P20_ORCA_SESSION:-}" != 1 ]]; then
  exec xvfb-run -a -s '-screen 0 1600x1000x24' dbus-run-session -- env P20_ORCA_SESSION=1 bash "$0" "$@"
fi
export NO_AT_BRIDGE=0
export GTK_MODULES=atk-bridge
export LANG=C.UTF-8
export XDG_RUNTIME_DIR="${RUNNER_TEMP:?}/gojet-desktop-$$"
mkdir -m 700 "$XDG_RUNTIME_DIR"
export P20_ORCA_LOG="${RUNNER_TEMP:?}/gojet-orca-$$.log"
# Keep full desktop debug logs out of artifacts: they can include fixture
# values. The probe retains only relevant control announcements.
openbox > "${RUNNER_TEMP}/gojet-openbox-$$.log" 2>&1 &
wm_pid=$!
orca_pid=''
cleanup() {
  if [[ -n "$orca_pid" ]]; then
    kill "$orca_pid" 2>/dev/null || true
    kill -KILL "$orca_pid" 2>/dev/null || true
    wait "$orca_pid" 2>/dev/null || true
  fi
  kill "$wm_pid" 2>/dev/null || true
  kill -KILL "$wm_pid" 2>/dev/null || true
  wait "$wm_pid" 2>/dev/null || true
  pulseaudio --kill 2>/dev/null || true
}
trap cleanup EXIT
# A CI runner has no physical audio device. Provide a real PulseAudio virtual
# sink for the real eSpeak synthesizer, not a dummy speech-dispatcher module.
pulseaudio --start --exit-idle-time=-1
pactl load-module module-null-sink sink_name=gojet_ci >/dev/null
pactl set-default-sink gojet_ci
# SSIPClient connects to the existing daemon or auto-spawns it. A repeated
# `speech-dispatcher --spawn` exits 1 when another native case already started
# the daemon. Require a real reachable eSpeak module instead of ignoring errors.
python3 - <<'PY'
import speechd
client = speechd.SSIPClient('gojet-p20-preflight')
try:
    assert any('espeak' in str(module).lower() for module in client.list_output_modules()), 'native eSpeak module unavailable'
finally:
    client.close()
PY
orca --replace --enable=speech --disable=braille --debug-file="$P20_ORCA_LOG" > "${RUNNER_TEMP}/gojet-orca-stderr-$$.log" 2>&1 &
orca_pid=$!
export P20_ORCA_PID="$orca_pid"
export P20_ORCA_VERSION="$(orca --version)"
for attempt in $(seq 1 30); do
  kill -0 "$orca_pid"
  if [[ -s "$P20_ORCA_LOG" ]]; then break; fi
  sleep 1
done
test -s "$P20_ORCA_LOG"
"$@"
