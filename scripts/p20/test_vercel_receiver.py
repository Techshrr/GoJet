import importlib.util
import json
import os
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('vercel_receiver_entry', Path(__file__).parent / 'vercel_receiver/api/index.py')
entry = importlib.util.module_from_spec(spec)
spec.loader.exec_module(entry)


class VercelReceiverTests(unittest.TestCase):
    def test_missing_configuration_cannot_pass(self):
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(entry.dispatch('GET', '/healthz', {}, b'')[0], 503)
            self.assertEqual(entry.dispatch('POST', '/deliver/test-run-01', {}, b'')[0], 503)

    def test_state_survives_receiver_instances_and_cas_retry(self):
        state = {'value': None, 'conflict': True}

        def command(args):
            if args[0] == 'GET':
                return state['value']
            self.assertEqual(args[0], 'EVAL')
            if state['conflict']:
                state['conflict'] = False
                return 0
            if (state['value'] or '') != args[4]:
                return 0
            state['value'] = args[5]
            return 1

        env = {'P20_RECEIVER_CONTROL_TOKEN': 'local-test-token-' + '0' * 32,
               'UPSTASH_REDIS_REST_URL': 'https://unused.example.test',
               'UPSTASH_REDIS_REST_TOKEN': 'unused-test-token'}
        headers = {'Authorization': 'Bearer ' + env['P20_RECEIVER_CONTROL_TOKEN']}
        with patch.dict(os.environ, env, clear=True):
            body = json.dumps({'secret': 'test-secret-' + 'x' * 32, 'workspace_id': 'ws-test'}).encode()
            self.assertEqual(entry.dispatch('POST', '/runs/test-run-01', headers, body, command)[0], 201)
            status, report = entry.dispatch('GET', '/runs/test-run-01', headers, b'', command)
            self.assertEqual(status, 200)
            self.assertEqual(report['delivery_count'], 0)
            self.assertNotIn('secret', report)


if __name__ == '__main__':
    unittest.main()
