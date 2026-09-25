#!/usr/bin/env python3
"""Correlated P17 Admin governance over the real T016 file; no mock principal."""
from __future__ import annotations

import base64
import hashlib
import hmac
import json
import os
import secrets
import struct
import time
from http.cookies import SimpleCookie
from pathlib import Path

from common import HEAD, emit, fail_if_errors
from t022_case import http_json, mysql, predecessor, q, scalar

CASE = 'P20-T023'
HANDOFF = Path('/tmp/gojet-p20-admin-handoff.json')
ORIGIN = 'https://admin.p17.test'


class ProofFailure(RuntimeError):
    pass


def require(value, message):
    if not value:
        raise ProofFailure(message)


def run_case():
    details = {'formal_p20_t023_claim': False, 'mock_authority': False,
               'test_header_authority': False, 'secret_material_recorded': False}
    errors = []
    try:
        t22, t16 = predecessor('P20-T022')['details'], predecessor('P20-T016')['details']
        require(t22.get('next_case') == CASE and t22.get('formal_p20_t022_claim') is True,
                'T022 did not formally unlock T023')
        user, workspace = t22['user_id'], t22['workspace_id']
        require(t16['user_id'] == user and t16['workspace_id'] == workspace,
                'T016/T022 identity correlation mismatch')
        file_id = int(t16['file_id'])
        require(file_id > 0 and t16.get('real_clamav_scan') is True, 'T016 real file authority missing')
        require(os.environ.get('GOJET_P20_ADMIN_HANDOFF') == str(HANDOFF), 'Admin fixture handoff not enabled')
        require(HANDOFF.stat().st_mode & 0o777 == 0o600, 'Admin handoff permissions are not private')
        handoff = json.loads(HANDOFF.read_text())
        HANDOFF.unlink()
        root_cookie = handoff['cookie_header']
        root_id = handoff['administrator_id']
        suffix = HEAD[:12]
        private_values = [root_cookie.split('=', 1)[1]]

        def call(method, path, cookie='', body=None, expected=200, headers=None):
            merged = {'Origin': ORIGIN}
            if cookie:
                merged['Cookie'] = cookie
            if headers:
                merged.update(headers)
            status, response_headers, result = http_json(method, path, body, merged)
            require(status == expected, f'{method} {path}: expected HTTP {expected}, observed {status}')
            require(isinstance(result, dict), 'Admin response is not an object')
            return response_headers, result

        def session(cookie):
            _, result = call('GET', '/api/admin/auth/session', cookie)
            require(bool(result.get('csrf_token')), 'Admin CSRF authority missing')
            private_values.append(result['csrf_token'])
            return result

        def mutate(path, cookie, body, key, expected=200, csrf_override=None):
            current = session(cookie)
            return call('POST', path, cookie, body, expected, {
                'X-CSRF-Token': current['csrf_token'] if csrf_override is None else csrf_override,
                'X-Correlation-ID': 'p20-t023-' + key + '-' + suffix,
                'Idempotency-Key': 'p20-t023-' + key + '-' + suffix,
            })[1]

        file_path = f'/api/admin/files/{file_id}'
        action_path = file_path + '/quarantine'
        call('GET', file_path, expected=401)
        root = session(root_cookie)
        require(root['administrator']['id'] == root_id and 'admins.manage' in root['permissions'],
                'T020 root identity/governance permission missing')
        require('files.manage' not in root['permissions'], 'root fixture unexpectedly owns file authority')
        mutate(action_path, root_cookie, {'reason': 'Verify explicit file permission boundary'}, 'no-permission', 403)
        permissions = ['files.manage', 'operations.manage', 'platform.read']
        role = mutate('/api/admin/roles', root_cookie, {
            'name': 'P20 T023 ' + suffix, 'description': 'Bounded formal file operations',
            'permissions': permissions, 'reason': 'Provision explicit formal T023 operator scope',
        }, 'role', 201)['role']
        password = 'P20!' + secrets.token_urlsafe(24)
        email = f'p20-t023-{suffix}@example.test'
        private_values.extend([password, email])
        administrator = mutate('/api/admin/administrators', root_cookie, {
            'email': email, 'display_name': 'P20 T023 Operator', 'password': password,
            'role_ids': [role['id']], 'reason': 'Provision formal T023 operator identity',
        }, 'administrator', 201)['administrator']
        response_headers, _ = call('POST', '/api/admin/auth/login', body={
            'email': email, 'password': password,
        }, headers={'X-Correlation-ID': 'p20-t023-login-' + suffix})
        cookie = ''
        for name, value in response_headers:
            if name.lower() == 'set-cookie':
                parsed = SimpleCookie(); parsed.load(value)
                if 'gojet_admin_session' in parsed:
                    cookie = 'gojet_admin_session=' + parsed['gojet_admin_session'].value
        require(bool(cookie), 'real Admin login did not issue a session')
        private_values.append(cookie.split('=', 1)[1])
        current = session(cookie)
        require(current['administrator']['id'] == administrator['id'] and
                set(current['permissions']) == set(permissions), 'scoped Admin identity/permissions mismatch')
        require(not current['session'].get('mfa_verified_at'), 'new Admin session unexpectedly has MFA')
        mutate(action_path, cookie, {'reason': 'Verify fresh MFA requirement'}, 'no-mfa', 428)
        enrollment = mutate('/api/admin/auth/totp/enroll', cookie, {}, 'enroll', 201)
        secret = enrollment['secret']; private_values.append(secret)
        key = base64.b32decode(secret.upper() + '=' * (-len(secret) % 8))
        digest = hmac.new(key, struct.pack('>Q', int(time.time()) // 30), hashlib.sha1).digest()
        offset = digest[-1] & 15
        code = str((struct.unpack('>I', digest[offset:offset + 4])[0] & 0x7fffffff) % 1000000).zfill(6)
        mutate('/api/admin/auth/totp/confirm', cookie, {'code': code}, 'confirm', 204)
        current = session(cookie)
        require(bool(current['session'].get('mfa_verified_at')), 'real Admin MFA confirmation missing')
        mutate(action_path, cookie, {'reason': ''}, 'no-reason', 422)
        mutate(action_path, cookie, {'reason': 'Verify CSRF protection'}, 'bad-csrf', 403, 'invalid')
        _, before_body = call('GET', file_path, cookie)
        before = before_body['file']
        require(before['workspace_id'] == workspace and before['id'] == file_id and
                before['scan_state'] == 'safe' and before['published'] is True,
                'Admin did not observe the real published T016 file')
        _, services = call('GET', '/api/admin/operations/services', cookie)
        expected_services = {'redirectengine', 'analyticsworker', 'analyticsreconciler', 'platformapi',
                             'mailworker', 'fileworker', 'operationsmonitor', 'logreceiver'}
        require({item['id'] for item in services['items']} == expected_services,
                'fixed eight-service operations inventory mismatch')
        audit_where = f"action='admin.file.quarantine' AND resource_type='file' AND resource_id={q(str(file_id))}"
        count = lambda: int(scalar('SELECT COUNT(*) FROM admin_audit_events WHERE ' + audit_where) or '0')
        audit_before = count()
        scans_before = int(scalar(f'SELECT COUNT(*) FROM file_scan_attempts WHERE file_id={file_id}') or '0')
        reason = 'Isolate correlated P20 file for formal administrator verification'
        after_body = mutate(action_path, cookie, {'reason': reason}, 'quarantine')
        after = after_body['file']
        require(after_body.get('replay') is False and after['scan_state'] == 'quarantined' and
                after['published'] is False and after['scan_generation'] == before['scan_generation'] + 1,
                'authorized file quarantine transition failed')
        replay = mutate(action_path, cookie, {'reason': reason}, 'quarantine')
        require(replay.get('replay') is True and replay['file'] == after, 'Admin idempotent replay failed')
        require(count() == audit_before + 1, 'Admin action did not write exactly one audit event')
        scans_after = int(scalar(f'SELECT COUNT(*) FROM file_scan_attempts WHERE file_id={file_id}') or '0')
        require(scans_after == scans_before + 1, 'quarantine/replay did not enqueue exactly one scan')
        require(scalar(f"SELECT CONCAT(scan_state,':',published,':',scan_generation) FROM files WHERE id={file_id} AND workspace_id={q(workspace)}") ==
                f"quarantined:0:{after['scan_generation']}", 'durable correlated file state mismatch')
        slug = scalar(f'SELECT public_slug FROM files WHERE id={file_id}')
        public_status, _, _ = http_json('GET', '/api/public/files/' + slug)
        require(public_status == 403, 'quarantined file remained publicly accessible')
        _, audit_page = call('GET', '/api/admin/audit?limit=500', cookie)
        matches = [item for item in audit_page['items'] if item.get('action') == 'admin.file.quarantine'
                   and item.get('resource_id') == str(file_id)
                   and item.get('request_id') == 'p20-t023-quarantine-' + suffix]
        require(len(matches) == 1, 'correlated Admin audit not available through authorized HTTP API')
        event = matches[0]
        require(event['actor_id'] == administrator['id'] and event['result'] == 'success' and
                event['reason'] == reason, 'Admin audit actor/reason/result attribution mismatch')
        serialized = json.dumps(event).lower()
        require(not any(value.lower() in serialized for value in private_values if value),
                'private Admin material leaked into action audit')
        audit_id = int(event['id'])
        before_digest = scalar(f"SELECT SHA2(CONCAT(actor_id,action,request_correlation_id,COALESCE(reason,''),before_json,after_json,metadata_json),256) FROM admin_audit_events WHERE id={audit_id}")
        for statement in (f'UPDATE admin_audit_events SET reason=reason WHERE id={audit_id}',
                          f'DELETE FROM admin_audit_events WHERE id={audit_id}'):
            denied = False
            try:
                mysql(statement)
            except RuntimeError as exc:
                denied = 'append-only' in str(exc)
            require(denied, 'database did not enforce append-only audit')
        require(scalar(f"SELECT SHA2(CONCAT(actor_id,action,request_correlation_id,COALESCE(reason,''),before_json,after_json,metadata_json),256) FROM admin_audit_events WHERE id={audit_id}") == before_digest,
                'append-only audit changed')
        mutate('/api/admin/auth/logout', cookie, {}, 'logout', 204)
        call('GET', file_path, cookie, expected=401)
        details.update({
            'formal_p20_t023_claim': True, 'user_id': user, 'workspace_id': workspace,
            'file_id': file_id, 'administrator_id': administrator['id'],
            't022_evidence_bound': True, 't016_real_file_bound': True,
            'real_platform_api': True, 'real_mysql': True, 'real_redis': True,
            'real_admin_login': True, 'real_totp_confirmation': True,
            'missing_permission_http_status': 403, 'missing_mfa_http_status': 428,
            'missing_reason_http_status': 422, 'invalid_csrf_http_status': 403,
            'revoked_session_http_status': 401, 'fixed_service_inventory_count': 8,
            'quarantine_http_status': 200, 'public_after_quarantine_http_status': public_status,
            'quarantine_persisted': True, 'replay_idempotent': True,
            'audit_write_delta': count() - audit_before, 'scan_write_delta': scans_after - scans_before,
            'audit_actor_reason_correlated': True, 'audit_append_only': True, 'audit_secret_safe': True,
            'next_case': 'P20-T024',
        })
    except ProofFailure as exc:
        errors.append(str(exc))
    except Exception as exc:
        errors.append('T023 harness error: ' + type(exc).__name__)
    finally:
        HANDOFF.unlink(missing_ok=True)
    return emit(CASE, 'p0', 'Real Admin operations action and audit workflow', errors, details)


if __name__ == '__main__':
    fail_if_errors([run_case()])
