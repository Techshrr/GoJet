#!/usr/bin/env python3
"""CI-only package builder: source, exact Go consumer probe, checksums."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import tempfile
import zipfile

from install import SOURCE_FILES

here = Path(__file__).resolve().parent
repo = here.parent.parent
source = os.environ["GITHUB_SHA"]
probe = here / "gojet-webrisk-probe"
if not probe.read_bytes().startswith(b"\x7fELF"):
    raise SystemExit("Native probe must be built before packaging")
output = repo / "artifacts/v10/P22/google-webrisk/GoJet-WebRisk-1.0.0.zip"
output.parent.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory() as temp:
    root = Path(temp) / "GoJet-WebRisk-1.0.0"
    root.mkdir()
    for name in SOURCE_FILES:
        if name in ("BUILD.json", "probe-main.go"):
            continue
        shutil.copyfile(here / name, root / name)
    shutil.copyfile(repo / "scripts/p22/webrisk_probe/main.go", root / "probe-main.go")
    (root / "BUILD.json").write_text(json.dumps({"source": source, "candidate_source": "32ea29d766b0ac0790964517ce4e220a1208bf0d",
        "target": "linux-amd64", "live_google_test": "NOT_RUN", "production_deployment": False}, indent=2) + "\n")
    (root / "SHA256SUMS").write_text("".join(hashlib.sha256((root / name).read_bytes()).hexdigest() + "  " + name + "\n"
                                           for name in sorted(SOURCE_FILES)))
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for file in sorted(root.iterdir()):
            archive.write(file, "GoJet-WebRisk-1.0.0/" + file.name)
print(json.dumps({"package": str(output), "sha256": hashlib.sha256(output.read_bytes()).hexdigest(), "source": source}))
