import importlib.util
import json
import os
from pathlib import Path
import unittest
from unittest.mock import patch, MagicMock

spec = importlib.util.spec_from_file_location('vercel_receiver_entry', Path(__file__).parent / 'vercel_receiver/api/index.py')
entry = importlib.util.module_from_spec(spec)
spec.loader.exec_module(entry)


class VercelReceiverTests(unittest.TestCase):
    def test_native_integration_url_without_rest_token(self):
        env = {'P20_RECEIVER_CONTROL_TOKEN': 'x' * 32,
               'UPSTASH_REDIS_REST_REDIS_URL': 'rediss://test.invalid:6379'}
        module = MagicMock()
        client = module.Redis.from_url.return_value.__enter__.return_value
        client.execute_command.return_value = True
        with patch.dict(os.environ, env, clear=True), patch.dict('sys.modules', {'redis': module}):
            status, report = entry.dispatch('GET', '/healthz', {}, b'')
            self.assertEqual(status, 200)
            self.assertTrue(report['checks']['redis_ping_passed'])
            client.execute_command.assert_called_once_with('PING')
            client.execute_command.return_value = 1
            self.assertEqual(entry.redis_command(['EVAL', entry.CAS, 1, 'key', '', '{}']), 1)

    def test_health_fails_closed_without_exposing_dependency_details(self):
        env = {'P20_RECEIVER_CONTROL_TOKEN': 'x' * 32,
               'UPSTASH_REDIS_REST_URL': 'https://unused.example.test',
               'UPSTASH_REDIS_REST_TOKEN': 'private-test-token'}
        def failed_command(args):
            raise ValueError('private-test-token')
        with patch.dict(os.environ, env, clear=True):
            status, report = entry.dispatch('GET', '/healthz', {}, b'', failed_command)
            self.assertEqual(status, 503)
            self.assertFalse(report['checks']['redis_ping_passed'])
            self.assertNotIn('private-test-token', json.dumps(report))
            self.assertEqual(entry.dispatch('GET', '/healthz', {}, b'', lambda _: 'PONG')[0], 200)
        env['P20_RECEIVER_CONTROL_TOKEN'] = 'short'
        with patch.dict(os.environ, env, clear=True):
            status, report = entry.dispatch('GET', '/healthz', {}, b'', lambda _: self.fail('must not contact Redis'))
            self.assertEqual(status, 503)
            self.assertFalse(report['checks']['control_token_length_valid'])

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
