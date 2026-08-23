from coherence_common import *

def validate_plan(errors: list[str]) -> None:
    req(PLAN.is_file(), "missing P09 test-plan.json", errors)
    if not PLAN.is_file():
        return
    plan = load(PLAN)
    ids = tuple(item.get("id") for item in plan.get("cases", []) if isinstance(item, dict))
    req(ids == EXPECTED_CASES, f"test-plan case order drift: {ids}", errors)
    req(plan.get("base_integration_commit") == "418277613cf4336273b19f5d0da8a47bc1d403d6", "P09 base integration drift", errors)
    closure = plan.get("closure_contract", {})
    req(closure.get("same_exact_head_required") is True, "closure no longer requires exact head", errors)
    req(closure.get("required_case_range") == "P09-T001..P09-T027", "closure range drift", errors)


def validate_producers(exact: str, errors: list[str]) -> dict[str, Any]:
    req(PRODUCERS.is_file(), "missing evidence-producer-manifest.json", errors)
    req(CONTRACT.is_file(), "missing exact-head contract artifact", errors)
    manifest: dict[str, Any] = {}
    if PRODUCERS.is_file():
        manifest = load(PRODUCERS)
        req(manifest.get("implementation_commit") == exact, f"producer manifest head={manifest.get('implementation_commit')} expected={exact}", errors)
        required = manifest.get("required_workflows", {})
        req(set(required) == set(REQUIRED_PRODUCERS), f"producer workflow set drift: {sorted(required) if isinstance(required, dict) else required}", errors)
        if isinstance(required, dict):
            for name in REQUIRED_PRODUCERS:
                record = required.get(name, {})
                req(record.get("head_sha") == exact, f"producer {name} head={record.get('head_sha')} expected={exact}", errors)
                req(record.get("status") == "completed" and record.get("conclusion") == "success", f"producer {name} not successful: {record}", errors)
                req(isinstance(record.get("run_id"), int) and record["run_id"] > 0, f"producer {name} run_id missing", errors)
        req(manifest.get("missing") == [] and manifest.get("pending") == [] and manifest.get("failed") == [], f"producer manifest unresolved: {manifest}", errors)
    contract: dict[str, Any] = {}
    if CONTRACT.is_file():
        contract = load(CONTRACT)
        req(contract.get("status") == "PASS" and contract.get("errors") == [], f"contract artifact not PASS: {contract}", errors)
        req(contract.get("implementation_commit") == exact, f"contract artifact head={contract.get('implementation_commit')} expected={exact}", errors)
        req(contract.get("case_range") == "P09-T001..P09-T027" and contract.get("case_count") == 27, f"contract case range/count drift: {contract}", errors)
        req(contract.get("base_integration_commit") == "418277613cf4336273b19f5d0da8a47bc1d403d6", "contract base integration drift", errors)
    return {"manifest": manifest, "contract": contract}


def load_cases(exact: str, errors: list[str]) -> tuple[dict[str, dict[str, Any]], list[dict[str, Any]]]:
    output: dict[str, dict[str, Any]] = {}
    entries: list[dict[str, Any]] = []
    for case_id in INPUT_CASES:
        path = case_path(case_id)
        req(path.is_file(), f"missing evidence {path}", errors)
        if not path.is_file():
            continue
        try:
            data = load(path)
        except Exception as exc:
            errors.append(f"invalid JSON {path}: {exc}")
            continue
        actual = data.get("case") or data.get("case_id")
        req(actual == case_id, f"{case_id} identity={actual}", errors)
        req(data.get("status") == "PASS", f"{case_id} status={data.get('status')}", errors)
        req(data.get("implementation_commit") == exact, f"{case_id} head={data.get('implementation_commit')} expected={exact}", errors)
        req(isinstance(data.get("errors"), list) and not data.get("errors"), f"{case_id} errors={data.get('errors')}", errors)
        output[case_id] = data
        entries.append({
            "case_id": case_id,
            "path": str(path.relative_to(ROOT)),
            "sha256": digest(path),
            "implementation_commit": data.get("implementation_commit"),
            "status": data.get("status"),
        })
    req(tuple(item["case_id"] for item in entries) == INPUT_CASES, "T001..T025 evidence set/order incomplete", errors)
    return output, entries

