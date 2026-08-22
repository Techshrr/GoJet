#!/usr/bin/env python3
from integration_common import *
from integration_cases_a import *
from integration_cases_b import *
from integration_cases_c import *

CASES = {
    "P09-T001": case_t001, "P09-T002": case_t002, "P09-T003": case_t003, "P09-T004": case_t004,
    "P09-T005": case_t005, "P09-T006": case_t006, "P09-T007": case_t007, "P09-T008": case_t008,
    "P09-T009": case_t009, "P09-T010": case_t010, "P09-T011": case_t011, "P09-T012": case_t012,
    "P09-T013": case_t013, "P09-T014": case_t014, "P09-T015": case_t015, "P09-T016": case_t016,
    "P09-T017": case_t017, "P09-T018": case_t018,
}

def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(CASE_IDS))
    args = parser.parse_args()
    case_id = args.case
    errors: list[str] = []
    observations = {}
    try:
        expect(WORKER.is_file(), f"native fileworker missing: {WORKER}")
        observations = CASES[case_id]()
    except Exception as exc:
        errors.append(f"{type(exc).__name__}: {exc}")
    path = write_evidence(case_id, observations, errors)
    print(path)
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print(f"{case_id} PASS on {HEAD}")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
