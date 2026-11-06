"""Admission for T024's protected route and live P09 health observations.

This validates evidence, never changes the frozen test plan or grants install
authority. P09 and the downstream P20 file-safety check share this boundary.
"""


def validate_handoff(value):
    def require(ok, reason):
        if not ok:
            raise ValueError('T024 '+reason)

    require(isinstance(value, dict), 'handoff missing')
    require(value.get('admin_route') == '/admin/platform/storage'
            and value.get('admin_state') == 'healthy', 'Admin health missing')
    for field in ['p17_completion_claimed', 'p22_completion_claimed', 'private_dependency_detail_leaked']:
        require(value.get(field) is False, field+' must be false')
    installer = value.get('installer')
    require(isinstance(installer, dict), 'protected Installer evidence missing')
    for field in ['certificate_verified_tls', 'real_unprivileged_filepreflight', 'state_not_advanced',
                  'root_completion_guard', 'starting_phases_are_fixtures', 'completion_marker_is_fixture',
                  'unchecked_p21_dependencies_are_false']:
        require(installer.get(field) is True, field+' missing')
    for field in ['privileged_apply_verified', 'full_installation_verified']:
        require(installer.get(field) is False, field+' exceeds this fixture scope')
    viewports = installer.get('viewports')
    require(isinstance(viewports, dict) and set(viewports) == {'desktop', 'mobile'}, 'viewport evidence incomplete')
    for name, record in viewports.items():
        require(isinstance(record, dict), name+' observations missing')
        for field, expected in {
            'unauthenticated_routes': 401, 'environment_route': 200, 'premature_services_health': 409,
            'services_status': 200, 'out_of_phase_environment_health': 409,
            'apply_posts': 0, 'unexpected_browser_errors': 0,
        }.items():
            require(type(record.get(field)) is int and record[field] == expected, name+' '+field+' drift')
        for field in ['stale_hidden', 'missing_hidden', 'recovered', 'layout_verified']:
            require(record.get(field) is True, name+' '+field+' missing')
        for field, storage, clamav in [('healthy', True, True), ('scanner_down', True, False),
                                       ('unsafe_storage', False, True)]:
            observation = record.get(field)
            require(isinstance(observation, dict) and observation.get('status') == 'failed'
                    and observation.get('storage') is storage and observation.get('clamav') is clamav,
                    name+' '+field+' health authority drift')
