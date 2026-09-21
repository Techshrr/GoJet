import hashlib
import hmac
import json
import time
import unittest

from webhook_receiver import Receiver


class ReceiverTests(unittest.TestCase):
    def setUp(self):
        self.token = 'receiver-test-control-token-00000000'
        self.secret = 'receiver-test-webhook-secret-000000'
        self.receiver = Receiver(self.token)
        self.control = {'Authorization': 'Bearer ' + self.token}
        self.run = '/runs/test-run-0001'
        status, _ = self.receiver.request('POST', self.run, self.control,
                                         json.dumps({'secret': self.secret, 'workspace_id': 'ws-test',
                                                     'fail_first': 1}).encode())
        self.assertEqual(status, 201)

    def deliver(self, secret):
        body = json.dumps({'id': 'link-audit-1', 'type': 'link.created',
                           'workspace_id': 'ws-test', 'data': {'link_id': 1, 'version': 1}}).encode()
        timestamp = str(int(time.time()))
        message = ('whd_test1\n' + timestamp + '\n').encode() + body
        headers = {'X-GoJet-Delivery': 'whd_test1', 'X-GoJet-Idempotency-Key': 'whd_test1',
                   'X-GoJet-Event': 'link.created', 'X-GoJet-Timestamp': timestamp,
                   'X-GoJet-Signature': 'v1=' + hmac.new(secret.encode(), message, hashlib.sha256).hexdigest()}
        return self.receiver.request('POST', '/deliver/test-run-0001', headers, body)[0]

    def test_retry_and_duplicate_report(self):
        self.assertEqual([self.deliver(self.secret) for _ in range(3)], [503, 200, 200])
        status, report = self.receiver.request('GET', self.run, self.control, b'')
        self.assertEqual(status, 200)
        self.assertEqual((report['attempt_count'], report['accepted_count'], report['delivery_count']), (3, 1, 1))
        self.assertNotIn(self.secret, json.dumps(report))

    def test_control_and_rotation(self):
        self.assertEqual(self.receiver.request('GET', self.run, {}, b'')[0], 401)
        replacement = 'receiver-replacement-secret-0000000'
        self.assertEqual(self.receiver.request('PATCH', self.run, self.control,
                                              json.dumps({'secret': replacement}).encode())[0], 200)
        self.assertEqual(self.deliver(self.secret), 401)
        self.assertEqual(self.deliver(replacement), 503)

    def test_cleanup(self):
        self.assertEqual(self.receiver.request('DELETE', self.run, self.control, b'')[0], 200)
        self.assertEqual(self.deliver(self.secret), 404)


if __name__ == '__main__':
    unittest.main()
