"""Negative log-attestation tests; fixtures are deliberately not CI authority."""
import hashlib
import json
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from scripts.ci.signed_authority import inspect_log


class RetainedAuthorityTests(unittest.TestCase):
    def fixture(self):
        report={'node':'P17','case':'P17-T035','implementation_commit':'a'*40,'status':'PASS','errors':[],
                'phase':'signed','review_phase':'signed','review_only_signed_child':True,
                'merge_authoritative':True,'signing_required':False,
                'defects':{'p0':0,'p1':0,'decision_required':0},
                'affected_matrix':{'complete':True,'passed':66,'required':66}}
        authority={'head':'a'*40,'case':'P17-T035','artifact':7,'archive_digest':'b'*64,
                   'matrix_count':66,'report_digest':hashlib.sha256(json.dumps(report,sort_keys=True,separators=(',',':')).encode()).hexdigest()}
        raw=(json.dumps(report,indent=2)+'\nSHA256 digest of uploaded artifact zip is '+'b'*64+'\n'
             +'Artifact gojet-v10-p17-closure-'+'a'*40+'.zip successfully finalized. Artifact ID 7\n').encode()
        return authority, raw

    def test_requires_unique_report_and_matching_upload_attestation(self):
        authority,raw=self.fixture()
        with patch('scripts.ci.signed_authority.AUTHORITIES',{'P17':authority}):
            self.assertEqual(inspect_log(raw,'P17')['status'],'PASS')
            for invalid in (raw+raw,raw.replace(b'Artifact ID 7',b'Artifact ID 8'),
                            raw.replace(b'b'*64,b'c'*64),raw.replace(b'"PASS"',b'"FAIL"')):
                with self.subTest(invalid=hashlib.sha256(invalid).hexdigest()), self.assertRaises(ValueError):
                    inspect_log(invalid,'P17')

    def test_report_digest_prevents_rewritten_signed_fields(self):
        authority,raw=self.fixture()
        with patch('scripts.ci.signed_authority.AUTHORITIES',{'P17':authority}):
            with self.assertRaisesRegex(ValueError,'digest mismatch'):
                inspect_log(raw.replace(b'"p1": 0',b'"p1": 1'),'P17')


if __name__=='__main__':
    unittest.main()
