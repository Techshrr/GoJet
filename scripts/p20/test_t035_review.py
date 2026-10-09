"""Synthetic admission boundary tests; never emit product evidence."""
import copy
import json
import tempfile
import unittest
from pathlib import Path
from t028_sources import digest
from t035_review import CRITERIA, validate_review


class ReviewAdmissionTest(unittest.TestCase):
    def fixture(self, root):
        source = root / 'frontend/reviewed.tsx'
        source.parent.mkdir()
        source.write_text('// synthetic test source')
        captures = {f'website/sample-{i}.png': digest(str(i).encode()) for i in range(109)}
        manifest = root / 'artifacts/v10/P20/runtime/t035-sources/collection.json'
        manifest.parent.mkdir(parents=True)
        manifest.write_text(json.dumps({'files': {p: {'sha256': h} for p, h in captures.items()}}))
        details = {'collection_sha256': digest(manifest.read_bytes())}
        review = dict(case='P20-T035', implementation_commit='a'*40, status='PASS',
                      role='Accessibility Reviewer', reviewer='Synthetic unit fixture', findings=[],
                      collection_sha256=details['collection_sha256'], captures=captures,
                      source_files={'frontend/reviewed.tsx': digest(source.read_bytes())},
                      criteria={cid: {'disposition': 'PASS', 'reason': 'Synthetic unit fixture only',
                                     'evidence': ['frontend/reviewed.tsx']} for cid in CRITERIA},
                      screen_reader_sample_scope_acknowledged=True)
        return details, review

    def test_complete_exact_binding(self):
        self.assertEqual(len(CRITERIA), 55)
        self.assertEqual(len(set(CRITERIA)), 55)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            details, review = self.fixture(root)
            result = validate_review(root, 'a'*40, details, review)
            self.assertEqual((result['criteria'], result['captures']), (55, 109))

    def test_reject_missing_stale_unsupported_and_waived_review(self):
        mutations = [lambda r: r.update(implementation_commit='b'*40),
                     lambda r: r.update(collection_sha256='stale'),
                     lambda r: r.update(reviewer=''),
                     lambda r: r.update(findings=['unresolved']),
                     lambda r: r['captures'].pop('website/sample-0.png'),
                     lambda r: r['captures'].update({'website/sample-0.png': 'stale'}),
                     lambda r: r['source_files'].update({'frontend/reviewed.tsx': 'stale'}),
                     lambda r: r.update(source_files={}),
                     lambda r: r.update(source_files={'': 'unsafe'}),
                     lambda r: r.update(source_files={'../outside.tsx': 'unsafe'}),
                     lambda r: r['criteria'].pop('1.1.1'),
                     lambda r: r['criteria']['1.1.1'].update(reason=''),
                     lambda r: r['criteria']['1.1.1'].update(evidence=['unreviewed.tsx']),
                     lambda r: r['criteria']['3.3.4'].update(disposition='NOT_APPLICABLE'),
                     lambda r: r.update(screen_reader_sample_scope_acknowledged=False)]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            details, review = self.fixture(root)
            for mutation in mutations:
                bad = copy.deepcopy(review)
                mutation(bad)
                with self.subTest(review=bad), self.assertRaises(ValueError):
                    validate_review(root, 'a'*40, details, bad)
            with self.assertRaises(ValueError): validate_review(root, 'a'*40, details, None)


if __name__ == '__main__': unittest.main()
