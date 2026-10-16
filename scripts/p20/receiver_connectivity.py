"""Real authenticated receiver storage check; never product delivery evidence."""
import json
import os
import secrets
import urllib.error
import urllib.request


def main():
    token = os.environ.get('P20_WEBHOOK_RECEIVER_TOKEN', '')
    if len(token) < 32:
        raise RuntimeError('GitHub receiver secret missing or too short')
    # Fixed authorized destination: never send the control token to a variable URL.
    base = 'https://test.gojet.cc'
    path = '/runs/config-' + secrets.token_hex(12)

    def call(method, endpoint, expected, data=None, authenticated=True):
        headers = {'Content-Type': 'application/json'}
        if authenticated:
            headers['Authorization'] = 'Bearer ' + token
        request = urllib.request.Request(base + endpoint, method=method, headers=headers,
                                         data=json.dumps(data).encode() if data is not None else None)
        # Do not forward credentials through redirects.
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args, **kwargs):
                return None
        try:
            response = urllib.request.build_opener(NoRedirect).open(request, timeout=20)
        except urllib.error.HTTPError as exc:
            response = exc
        with response:
            if response.code != expected:
                raise RuntimeError(f'{method} receiver returned HTTP {response.code}; expected {expected}')
            return json.load(response)

    created = False
    try:
        call('GET', path, 401, authenticated=False)
        call('POST', path, 201, {'secret': secrets.token_hex(32),
                                'workspace_id': 'p20-config-probe', 'fail_first': 1})
        created = True
        report = call('GET', path, 200)
        if any(report.get(key) != 0 for key in ('delivery_count', 'accepted_count', 'attempt_count', 'invalid_count')):
            raise RuntimeError('new receiver run was not empty')
        call('PATCH', path, 200, {'secret': secrets.token_hex(32)})
        call('GET', path, 200)
    finally:
        if created:
            call('DELETE', path, 200)
    call('GET', path, 404)
    print(json.dumps({'receiver_authenticated_storage_passed': True,
                      'cleanup_passed': True, 'formal_p20_t024_claim': False}))


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as exc:
        print(str(exc))
        raise SystemExit(1)
    except Exception as exc:
        print('Receiver connectivity failed: ' + type(exc).__name__)
        raise SystemExit(1)
