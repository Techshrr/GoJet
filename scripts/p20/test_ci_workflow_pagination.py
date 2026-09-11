"""Offline regression for closure discovery; no dispatch or live credentials."""
import ast
import unittest
import urllib.parse
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def load_discovery(phase, request):
    path = ROOT / f"scripts/p{phase}/closure_ci.py"
    name = "fetch_runs" if phase < 16 else "runs" if phase == 16 else "exact_runs"
    tree = ast.parse(path.read_text())
    function = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == name)
    namespace = {
        "urllib": urllib, "HEAD": "candidate-sha", "REPO": "owner/repo",
        "REPOSITORY": "owner/repo", "api": request, "request_json": request,
    }
    exec(compile(ast.Module(body=[function], type_ignores=[]), str(path), "exec"), namespace)
    return namespace[name]


class WorkflowDiscoveryTests(unittest.TestCase):
    def test_all_pages_preserve_pending_and_failed_runs(self):
        for phase in range(14, 20):
            for size in (0, 99, 100, 130, 200, 999):
                with self.subTest(phase=phase, size=size):
                    rows = [{"id": i, "status": "queued" if i % 2 else "completed",
                             "conclusion": None if i % 2 else "failure"} for i in range(size)]
                    calls = []

                    def request(url):
                        query = urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)
                        self.assertEqual(query["head_sha"], ["candidate-sha"])
                        self.assertEqual(query["per_page"], ["100"])
                        page = int(query["page"][0])
                        calls.append(page)
                        return {"workflow_runs": rows[(page - 1) * 100:page * 100]}

                    self.assertEqual(load_discovery(phase, request)(), rows)
                    self.assertEqual(calls, list(range(1, size // 100 + 2)))

    def test_api_failure_is_not_missing_authority(self):
        for phase in range(14, 20):
            def request(url):
                if urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)["page"] == ["1"]:
                    return {"workflow_runs": [{"id": i} for i in range(100)]}
                raise OSError("API unavailable")
            with self.subTest(phase=phase), self.assertRaises(OSError):
                load_discovery(phase, request)()

    def test_malformed_and_capped_listing_fail_closed(self):
        for phase in range(14, 20):
            for payload in ({}, {"workflow_runs": None}, {"workflow_runs": [{}] * 100}):
                with self.subTest(phase=phase, payload_type=type(payload.get("workflow_runs"))), self.assertRaises((KeyError, RuntimeError)):
                    load_discovery(phase, lambda url: payload)()


if __name__ == "__main__":
    unittest.main()
