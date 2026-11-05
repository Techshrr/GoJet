"""Index rejection tests using synthetic package bytes, never native-test evidence."""
import json
from pathlib import Path
import tempfile
import unittest
from archive import BINS, digest, encode, write_archive
from finalize_candidate import REQUIRED, SOURCE_REPORTS, finalize
from runbooks import RUNBOOKS, validate

ROOT = Path(__file__).resolve().parents[2]
SOURCE = 'a'*40
CONTEXT = {'repository': 'Techshrr/GoJet', 'job': 'payload', 'run_id': '1', 'run_attempt': '1'}


class CandidateTests(unittest.TestCase):
    def fixture(self, root):
        files = {'bin/'+name: b'synthetic executable '+name.encode() for name in BINS}
        files.update({'deploy/systemd/gojet-'+name+'.service': b'synthetic unit' for name in BINS-{'filepreflight'}})
        files.update({'static/'+app+'/index.html': b'synthetic page' for app in ('site', 'docs', 'workspace', 'admin')})
        files.update({'runbooks/'+name: b'# Synthetic\n\n```bash\ntrue\n```\n' for name in RUNBOOKS})
        catalog = json.loads((ROOT/'artifacts/v10/P21/upgrade/compatibility-native-installation.json').read_text())
        # Use the real number of migrations, but synthetic immutable content.
        migrations = []
        for number in range(1, catalog['native_migrations_applied']+1):
            path = f'migrations/{number:06d}_fixture.sql'
            files[path] = b'SELECT 1;'
            migrations.append({'path': path, 'sha256': digest(files[path])})
        migration_catalog = {'migrations': migrations}
        files['migration-catalog.json'] = encode(migration_catalog)
        archive = root/'candidate.tar.gz'
        components = [{'type': 'file', 'name': name, 'hashes': [{'alg': 'SHA-256', 'content': digest(data)}]} for name, data in files.items()]
        result = write_archive(files, archive, SOURCE, '10.0.0-rc.1', components)
        (root/(archive.name+'.sha256')).write_text(result['archive_sha256']+'  '+archive.name+'\n')
        import tarfile
        with tarfile.open(archive, 'r:gz') as stream:
            manifest_sha = digest(stream.extractfile('manifest.json').read())
        build = {'source': SOURCE, 'version': '10.0.0-rc.1', 'migration_catalog': migration_catalog,
                 'native_version_checks': [{'service': name, 'commit': SOURCE, 'version': '10.0.0-rc.1',
                    'sha256': digest(files['bin/'+name])} for name in sorted(BINS)]}
        reports = {'verification.json': dict(result, reproducible_archive=True, runbooks=validate(files)),
                   'native-build.json': build}
        for name, checks in REQUIRED.items():
            reports[name] = {check: True for check in checks}
            if name in SOURCE_REPORTS: reports[name]['source'] = SOURCE
        # Archived successful reports provide independently maintained field shapes;
        # rebind these copies ONLY to exercise rejection logic with synthetic bytes.
        reports['installation-preparation.json'] = catalog
        reports['installation-preparation.json']['source'] = SOURCE
        reports['risk-projection-recovery.json'] = json.loads((ROOT/'artifacts/v10/P21/upgrade/risk-projection-recovery.json').read_text())
        for name in ('target-integrity.json', 'installation-preparation.json'):
            reports[name].update(archive_sha256=result['archive_sha256'], manifest_sha256=manifest_sha)
        reports['dependency-preflight.json']['baseline'] = {str(n): True for n in range(8)}
        for name, value in reports.items(): (root/name).write_bytes(encode(value))
        return archive

    def run_index(self, root, archive):
        return finalize(archive, root/'native-build.json', SOURCE, CONTEXT)

    def test_index_binds_bytes_and_keeps_gate_unapproved(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); archive = self.fixture(root)
            before = archive.read_bytes()
            result = self.run_index(root, archive)
            self.assertEqual(archive.read_bytes(), before)
            self.assertEqual(result['archive']['sha256'], digest(before))
            self.assertEqual(result['evidence']['installation-preparation.json']['sha256'], digest((root/'installation-preparation.json').read_bytes()))
            self.assertFalse(result['installable'])
            self.assertFalse(result['production_acceptance'])
            self.assertEqual(result['g11_approval'], 'pending-technical-review')

    def test_rejects_foreign_incomplete_or_failed_evidence(self):
        cases = (
            ('target-integrity.json', lambda r: r.update(archive_sha256='b'*64)),
            ('installation-preparation.json', lambda r: r.update(source='b'*40)),
            ('static-routing.json', lambda r: r.update(four_static_apps=False)),
            ('native-build.json', lambda r: r['native_version_checks'][0].update(sha256='b'*64)),
            ('installation-preparation.json', lambda r: r['upgrade']['interrupted_phases'].pop()),
            ('installation-preparation.json', lambda r: r['upgrade']['incompatible_candidates_rejected_before_mutation'].pop()),
            ('installation-preparation.json', lambda r: r['upgrade'].update(production_acceptance=True)),
            ('risk-projection-recovery.json', lambda r: r.update(links=100)),
            ('dependency-preflight.json', lambda r: r['baseline'].update({'0': False})),
        )
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); archive = self.fixture(root)
            for name, mutate in cases:
                with self.subTest(report=name, mutation=mutate):
                    path = root/name; raw = path.read_bytes(); report = json.loads(raw)
                    mutate(report); path.write_bytes(encode(report))
                    with self.assertRaises(ValueError): self.run_index(root, archive)
                    path.write_bytes(raw)
            (root/'risk-projection-recovery.json').unlink()
            with self.assertRaises(ValueError): self.run_index(root, archive)

    def test_rejects_altered_archive_and_companion(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); archive = self.fixture(root)
            companion = root/(archive.name+'.sha256'); original = companion.read_bytes()
            companion.write_text('b'*64+'  '+archive.name+'\n')
            with self.assertRaises(ValueError): self.run_index(root, archive)
            companion.write_bytes(original)
            archive.write_bytes(archive.read_bytes()+b'changed')
            with self.assertRaises(ValueError): self.run_index(root, archive)
