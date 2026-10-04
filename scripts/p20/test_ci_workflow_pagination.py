"""Offline regression for closure discovery; no dispatch or live credentials."""
import ast
from datetime import datetime
import unittest
import urllib.parse
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def load_discovery(phase, request):
    path = ROOT / ("scripts/p15/coherence_ci.py" if phase == "15_coherence" else f"scripts/p{phase}/closure_ci.py")
    name = "exact_producer_runs" if phase == "15_coherence" else "fetch_runs" if phase < 16 else "runs" if phase == 16 else "exact_runs"
    tree = ast.parse(path.read_text())
    function = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == name)
    namespace = {
        "urllib": urllib, "HEAD": "candidate-sha", "REPO": "owner/repo",
        "REPOSITORY": "owner/repo", "api": request, "request_json": request, "api_get": request,
    }
    exec(compile(ast.Module(body=[function], type_ignores=[]), str(path), "exec"), namespace)
    return namespace[name]


class WorkflowDiscoveryTests(unittest.TestCase):
    def test_all_pages_preserve_pending_and_failed_runs(self):
        for phase in (*range(14, 20), "15_coherence"):
            for size in (0, 99, 100, 130, 200, 999):
                with self.subTest(phase=phase, size=size):
                    rows = [{"id": i, "status": "queued" if i % 2 else "completed",
                             "conclusion": None if i % 2 else "failure"} for i in range(size)]
                    calls = []

                    def request(url):
                        query = urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)
                        self.assertEqual(query["head_sha"], ["candidate-sha"])
                        self.assertEqual(query["per_page"], ["100"])
                        if phase == "15_coherence":
                            self.assertEqual(query["event"], ["pull_request"])
                        page = int(query["page"][0])
                        calls.append(page)
                        return {"workflow_runs": rows[(page - 1) * 100:page * 100]}

                    self.assertEqual(load_discovery(phase, request)(), rows)
                    self.assertEqual(calls, list(range(1, size // 100 + 2)))

    def test_api_failure_is_not_missing_authority(self):
        for phase in (*range(14, 20), "15_coherence"):
            def request(url):
                if urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)["page"] == ["1"]:
                    return {"workflow_runs": [{"id": i} for i in range(100)]}
                raise OSError("API unavailable")
            with self.subTest(phase=phase), self.assertRaises(OSError):
                load_discovery(phase, request)()

    def test_malformed_and_capped_listing_fail_closed(self):
        for phase in (*range(14, 20), "15_coherence"):
            for payload in ({}, {"workflow_runs": None}, {"workflow_runs": [{}] * 100}):
                with self.subTest(phase=phase, payload_type=type(payload.get("workflow_runs"))), self.assertRaises((KeyError, RuntimeError)):
                    load_discovery(phase, lambda url: payload)()


class CurrentAttemptArtifactTests(unittest.TestCase):
    def select(self, artifacts, boundary="2026-09-12T11:22:31Z"):
        path = ROOT / "scripts/p15/coherence_ci.py"
        tree = ast.parse(path.read_text())
        function = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == "artifact_for")
        namespace = {"datetime": datetime, "REPOSITORY": "owner/repo",
                     "api_get": lambda url: {"artifacts": artifacts}}
        exec(compile(ast.Module(body=[function], type_ignores=[]), str(path), "exec"), namespace)
        return namespace["artifact_for"](1, "exact", "contract", created_after=boundary)

    def row(self, identity, created):
        return {"id": identity, "name": "contract", "created_at": created,
                "digest": "sha256:fixture", "size_in_bytes": 12}

    def test_previous_attempt_is_not_current_authority(self):
        old = self.row(1, "2026-09-12T11:00:39Z")
        current = self.row(2, "2026-09-12T11:23:00Z")
        self.assertIsNone(self.select([old]))
        self.assertEqual(self.select([old, current])["id"], 2)
        self.assertEqual(self.select([current, old])["id"], 2)

    def test_duplicates_in_current_attempt_remain_ambiguous(self):
        self.assertIsNone(self.select([self.row(2, "2026-09-12T11:23:00Z"),
                                      self.row(3, "2026-09-12T11:23:01Z")]))

    def test_missing_or_naive_timestamps_fail_closed(self):
        for stamp in ("invalid", "2026-09-12T11:23:00"):
            with self.subTest(stamp=stamp), self.assertRaises((ValueError, RuntimeError)):
                self.select([self.row(2, stamp)])


if __name__ == "__main__":
    unittest.main()
