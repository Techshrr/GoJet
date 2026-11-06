"""Execute candidate checks on the frozen P21 lineage without issuing a signature.

The original P20 candidate/signing guard and T049 oracle remain unchanged.
This entry point admits regression execution only, with explicit provenance.
"""
import subprocess

from candidate_phase import verify
from closure_ci import review_fields, review_only
from t028_sources import require

P20_PRIOR = '6e628b9879eb4dddf335a324e4f4d7ae3a77cd5c'
P20_SIGNED = '007b20244081a9d0336c19262e0f095c9d101976'
P20_INTEGRATION = 'c5b51ae9dbc01da5da38f2cee7d87e6a3122c3dd'
P21_AUTHORITY = 'a7a3d1258a63a47ef59ac2330fcfd881c161d7c7'
FROZEN = (
    'artifacts/v10/P20/review.md', 'artifacts/v10/P20/test-plan.json',
    'artifacts/v10/P21/test-plan.json', 'scripts/p21/validate_contract.py',
    '.github/workflows/p21-native-package.yml', 'toolchain.lock.json',
    'specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md',
    'specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md',
    'specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md',
)


def verify_execution(data, root, head):
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=root, text=True).strip()

    require(git('rev-parse', 'HEAD') == head, 'execution checkout/head mismatch')
    result = subprocess.run(['git', 'merge-base', '--is-ancestor', P21_AUTHORITY, head],
                            cwd=root, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    if result.returncode != 0:
        # Earlier P20 candidates keep their original strict phase admission.
        # A missing/shallow anchor never enables the later regression mode.
        verify(data, root, head)
        return {'mode': 'p20-candidate-execution', 'implementation_commit': head,
                'merge_authoritative': False, 'formal_p20_t049_claim': False}

    require(data['implementation_commit'] == head and data['status'] == 'PASS'
            and data['errors'] == [] and data['frozen_contract_preserved'] is True
            and data['merge_authoritative'] is False and data['review_phase'] == 'signed'
            and data['mode'] == 'signed-review-guard' and data['implementation_authorized'] is False,
            'invalid inherited P20 contract for regression execution')
    require(git('rev-list', '--parents', '-n', '1', P21_AUTHORITY).split()
            == [P21_AUTHORITY, P20_INTEGRATION], 'P21 contract parent mismatch')
    require(git('rev-list', '--parents', '-n', '1', P20_INTEGRATION).split()
            == [P20_INTEGRATION, P20_PRIOR, P20_SIGNED], 'P20 integration lineage mismatch')
    bindings = {}
    for path in FROZEN:
        expected = git('rev-parse', P21_AUTHORITY + ':' + path)
        require(git('rev-parse', head + ':' + path) == expected,
                'frozen regression authority changed: ' + path)
        original = subprocess.check_output(['git', 'show', P21_AUTHORITY + ':' + path], cwd=root)
        target = root / path
        require(target.is_file() and not target.is_symlink() and target.read_bytes() == original,
                'working-tree regression authority changed: ' + path)
        bindings[path] = expected
    review_path = 'artifacts/v10/P20/review.md'
    require(git('rev-parse', P20_SIGNED + ':' + review_path) == bindings[review_path],
            'original P20 signed review was replaced')
    fields = review_fields((root / review_path).read_text())
    review_only(root, P20_SIGNED, fields['head'])
    return {'mode': 'p21-regression-only', 'implementation_commit': head,
            'p20_integration_commit': P20_INTEGRATION, 'p20_signed_commit': P20_SIGNED,
            'p20_pre_sign_commit': fields['head'], 'p21_contract_authority': P21_AUTHORITY,
            'frozen_blobs': bindings, 'merge_authoritative': False,
            'formal_p20_t049_claim': False, 'new_signature_issued': False,
            'p21_p22_completion_claim': False}
