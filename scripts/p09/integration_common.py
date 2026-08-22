from integration_env import *

def run_worker(address: str = REAL_CLAMD, *, max_jobs: int = 1, scan_timeout: str = "10s",
               dial_timeout: str = "1s", signature_age: str = "72h", claim_lease: str = "2s",
               worker_id: str | None = None, timeout: float = 45.0):
    env = os.environ.copy()
    env.update({
        "GOJET_CLAMAV_NETWORK": "tcp",
        "GOJET_CLAMAV_ADDRESS": address,
        "GOJET_CLAMAV_DIAL_TIMEOUT": dial_timeout,
        "GOJET_CLAMAV_SCAN_TIMEOUT": scan_timeout,
        "GOJET_CLAMAV_MAX_SIGNATURE_AGE": signature_age,
        "GOJET_FILE_SCAN_CLAIM_LEASE": claim_lease,
        "GOJET_FILE_WORKER_POLL_INTERVAL": "50ms",
        "GOJET_FILE_WORKER_MAX_JOBS": str(max_jobs),
        "GOJET_FILE_WORKER_ID": worker_id or f"p09-worker-{uuid.uuid4().hex[:12]}",
    })
    completed = subprocess.run([str(WORKER)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
    if completed.returncode != 0:
        raise Failure(f"fileworker failed rc={completed.returncode}: {completed.stdout[-4000:]}")
    return completed.stdout

def worker_popen(address: str, *, scan_timeout: str = "10s", claim_lease: str = "2s", worker_id: str | None = None):
    env = os.environ.copy()
    env.update({
        "GOJET_CLAMAV_NETWORK": "tcp",
        "GOJET_CLAMAV_ADDRESS": address,
        "GOJET_CLAMAV_DIAL_TIMEOUT": "500ms",
        "GOJET_CLAMAV_SCAN_TIMEOUT": scan_timeout,
        "GOJET_CLAMAV_MAX_SIGNATURE_AGE": "72h",
        "GOJET_FILE_SCAN_CLAIM_LEASE": claim_lease,
        "GOJET_FILE_WORKER_POLL_INTERVAL": "50ms",
        "GOJET_FILE_WORKER_MAX_JOBS": "1",
        "GOJET_FILE_WORKER_ID": worker_id or f"p09-worker-{uuid.uuid4().hex[:12]}",
    })
    return subprocess.Popen([str(WORKER)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)

def terminate(proc: subprocess.Popen) -> None:
    if proc.poll() is not None:
        return
    proc.terminate()
    try:
        proc.wait(timeout=3)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=3)

def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])

@contextlib.contextmanager
def fault_server(mode: str, *, hold_seconds: float = 2.0):
    port = free_port()
    cmd = [sys.executable, str(FAULT_SERVER), "--mode", mode, "--port", str(port), "--hold-seconds", str(hold_seconds)]
    proc = subprocess.Popen(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    try:
        line = proc.stdout.readline().strip() if proc.stdout else ""
        expect("READY" in line, f"fault server did not become ready: {line}")
        yield f"127.0.0.1:{port}", proc
    finally:
        terminate(proc)

def wait_until(predicate, timeout: float, description: str, interval: float = 0.1):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        last = predicate()
        if last:
            return last
        time.sleep(interval)
    raise Failure(f"timeout waiting for {description}; last={last!r}")

def db_resource(file_id: int) -> dict:
    raw = mysql(
        "SELECT id,workspace_id,public_slug,original_name,storage_key,size_bytes,content_sha256,"
        "declared_mime,detected_mime,scan_state,scan_generation,published,download_count "
        f"FROM files WHERE id={int(file_id)}"
    )
    expect(raw != "", f"file {file_id} missing from database")
    fields = raw.split("\t")
    expect(len(fields) == 13, f"unexpected file row: {raw}")
    keys = ["id","workspace_id","public_slug","original_name","storage_key","size_bytes","content_sha256",
            "declared_mime","detected_mime","scan_state","scan_generation","published","download_count"]
    data = dict(zip(keys, fields))
    for key in ("id","size_bytes","scan_generation","published","download_count"):
        data[key] = int(data[key])
    return data

def db_scan(file_id: int) -> dict:
    raw = mysql(
        "SELECT status,COALESCE(engine_version,''),COALESCE(signature_version,''),"
        "COALESCE(verdict_code,''),COALESCE(error_code,''),generation "
        f"FROM file_scan_attempts WHERE file_id={int(file_id)} ORDER BY generation DESC LIMIT 1"
    )
    expect(raw != "", f"scan attempt missing for {file_id}")
    fields = raw.split("\t")
    expect(len(fields) == 6, f"unexpected scan row: {raw}")
    return dict(zip(["status","engine_version","signature_version","verdict_code","error_code","generation"], fields))

def storage_path(kind: str, key: str) -> Path:
    return STORAGE_ROOT / kind / key[:2] / key[2:4] / key

def public_binary(slug: str, cookie: str | None = None):
    headers = {}
    if cookie:
        headers["Cookie"] = cookie
    return request("GET", f"/api/public/files/{urllib.parse.quote(slug, safe='')}", headers=headers)

def write_evidence(case_id: str, observations: dict, errors: list[str] | None = None):
    directory = CLAM_ROOT if case_id in {f"P09-T{i:03d}" for i in range(5, 11)} else RESULT_ROOT
    directory.mkdir(parents=True, exist_ok=True)
    payload = {
        "case": case_id,
        "status": "PASS" if not errors else "FAIL",
        "implementation_commit": HEAD,
        "errors": errors or [],
        "observations": observations,
    }
    path = directory / f"{case_id}.json"
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return path

