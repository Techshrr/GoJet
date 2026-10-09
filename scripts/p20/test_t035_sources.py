"""Synthetic boundary tests, never emitted as browser evidence."""
import copy
import io
import json
import tempfile
import unittest
import zipfile
from pathlib import Path
from t028_sources import digest
from t035_sources import PRODUCERS, SIZES, INTERACTIONS, MENU_TRACES, save_archive, prerequisite_member
from t035_matrix import inspect_collection


class AccessibilitySourcesTest(unittest.TestCase):
    def test_archive_path_and_duplicate_rejection(self):
        for path in ['/browser/P20-T034.json', '../browser/P20-T034.json',
                     'runtime/t034-sources/../escape', 'runtime//t034-sources/file']:
            with self.subTest(path=path), self.assertRaises(ValueError):
                prerequisite_member(path)
        for paths in [('browser/P20-T034.json', 'browser/P20-T034.json'), ('unrelated.json',)]:
            raw = io.BytesIO()
            with zipfile.ZipFile(raw, 'w') as archive:
                import warnings
                with warnings.catch_warnings():
                    warnings.simplefilter('ignore', UserWarning)
                    for path in paths: archive.writestr(path, '{}')
            with tempfile.TemporaryDirectory() as temp, zipfile.ZipFile(raw) as archive:
                with self.assertRaises(ValueError):
                    save_archive(Path(temp), {'files': {}}, 'prerequisite', archive)

    def fixture(self, directory):
        head = 'a' * 40
        manifest = {'implementation_commit': head, 'artifacts': {}, 'files': {}}
        for key, (workflow, _, _, surfaces) in PRODUCERS.items():
            manifest['artifacts'][key] = dict(workflow=workflow, head_sha=head, run_id=1,
                run_attempt=1, artifact_id=1, archive_sha256='sha256:' + 'b' * 64)
            paths = ['prerequisite/artifacts/v10/P20/browser/P20-T034.json'] if surfaces is None else [
                f'{key}/{surface}{suffix}' for surface in surfaces for suffix in ['.json'] +
                [f'-{size}-{theme}.png' for size in SIZES for theme in ('light', 'dark')]]
            if surfaces is not None:
                paths += [f'{key}/interactions/{case}.json' for case in INTERACTIONS[key]]
            if key == 'website':
                paths += ['website/menu-traces/' + name for name in MENU_TRACES]
            if key == 'workspace':
                paths += ['workspace/interactions/P10-T017-delete-confirmation.png']
            for path in paths:
                target = directory / path
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(b'synthetic boundary fixture')
                manifest['files'][path] = {'producer': key, 'sha256': digest(target.read_bytes())}
        return head, manifest

    def test_manifest_binding_and_raw_members(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            head, manifest = self.fixture(directory)
            inspect_collection(directory, manifest, head)
            mutations = [lambda m: m.update(implementation_commit='other'),
                lambda m: m['artifacts'].pop('auth'),
                lambda m: m['artifacts']['auth'].update(head_sha='other'),
                lambda m: m['artifacts']['auth'].update(workflow='other.yml'),
                lambda m: m['artifacts']['auth'].update(run_attempt=0),
                lambda m: m['artifacts']['auth'].update(archive_sha256='forged'),
                lambda m: m['files'].pop('auth/auth.json'),
                lambda m: m['files']['auth/auth.json'].update(sha256='forged'),
                lambda m: m['files']['auth/auth.json'].update(producer='website')]
            for mutate in mutations:
                bad = copy.deepcopy(manifest); mutate(bad)
                with self.assertRaises(ValueError): inspect_collection(directory, bad, head)
            (directory / 'untracked.json').write_text('{}')
            with self.assertRaisesRegex(ValueError, 'untracked'):
                inspect_collection(directory, manifest, head)


if __name__ == '__main__':
    unittest.main()
