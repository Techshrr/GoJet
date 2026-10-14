"""Negative provenance cases; fabricated metadata is test input, never CI evidence."""
import unittest
from release_matrix import admit_run, select_latest, REPOSITORY


class MatrixAdmissionTests(unittest.TestCase):
    def run_record(self, identity=1, conclusion='success', workflow='p01-engineering.yml'):
        return {'id':identity,'head_sha':'a'*40,'path':'.github/workflows/'+workflow,
                'event':'workflow_dispatch','head_repository':{'full_name':REPOSITORY},
                'status':'completed','conclusion':conclusion,'run_attempt':1}

    def job(self, identity=1, conclusion='success', name='native', run_id=1):
        return {'id':identity,'run_id':run_id,'name':name,'status':'completed','conclusion':conclusion}

    def test_new_failure_cannot_hide_behind_old_green(self):
        selected=select_latest([self.run_record(),self.run_record(2,'failure')],'p01-engineering.yml','a'*40)
        self.assertEqual(selected['id'],2)
        with self.assertRaises(ValueError):admit_run(selected,[self.job(run_id=2)],'p01-engineering.yml','a'*40)

    def test_foreign_head_repository_and_job_rejected(self):
        run=self.run_record()
        self.assertIsNone(select_latest([run],'p01-engineering.yml','b'*40))
        run['head_repository']={'full_name':'other/GoJet'}
        self.assertIsNone(select_latest([run],'p01-engineering.yml','a'*40))
        with self.assertRaises(ValueError):admit_run(self.run_record(),[self.job(run_id=2)],'p01-engineering.yml','a'*40)

    def test_retry_uses_newest_job_and_rejects_skip(self):
        with self.assertRaises(ValueError):admit_run(self.run_record(),[self.job(),self.job(2,'failure')],'p01-engineering.yml','a'*40)
        with self.assertRaises(ValueError):admit_run(self.run_record(),[self.job(conclusion='skipped')],'p01-engineering.yml','a'*40)
        self.assertEqual(admit_run(self.run_record(),[self.job(conclusion='failure'),self.job(2)],'p01-engineering.yml','a'*40)['jobs'][0]['id'],2)

    def test_historical_signing_is_not_current_contract_authority(self):
        workflow='p14-support-tickets-mail.yml'
        run=self.run_record(conclusion='failure',workflow=workflow)
        result=admit_run(run,[self.job(name='P14 contract freeze'),self.job(2,'failure','P14 exact-head accountable closure')],workflow,'a'*40)
        self.assertEqual(result['admission_scope'],'P14 contract freeze')
        with self.assertRaises(ValueError):admit_run(run,[self.job(conclusion='failure',name='P14 contract freeze')],workflow,'a'*40)


if __name__=='__main__':unittest.main()
