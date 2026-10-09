#!/usr/bin/env bash
set -euo pipefail
# Native desktop tooling only; no product dependencies or generated oracle.
sudo apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq orca speech-dispatcher speech-dispatcher-espeak-ng espeak-ng xvfb xauth dbus-x11 openbox
orca --version
speech-dispatcher --version
