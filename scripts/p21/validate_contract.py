"""Immutable P21 entry contract; never emits package/installation PASS."""
import json
import subprocess
from pathlib import Path
BASE = 'c5b51ae9dbc01da5da38f2cee7d87e6a3122c3dd'
FILES = ['artifacts/v10/P21/test-plan.json','artifacts/v10/P21/review.md','scripts/p21/validate_contract.py','.github/workflows/p21-native-package.yml']
def git(*args):
    return subprocess.check_output(['git',*args],text=True).strip()
def main():
    head=git('rev-parse','HEAD')
    subprocess.run(['git','merge-base','--is-ancestor',BASE,head],check=True)
    commits=git('rev-list','--reverse','--ancestry-path',BASE+'..'+head).splitlines()
    assert commits, 'missing frozen contract commit'
    authority=commits[0]
    assert git('rev-list','--parents','-n','1',authority).split()==[authority,BASE]
    assert set(git('diff','--name-only',BASE,authority).splitlines())==set(FILES)
    for path in FILES:
        if path.endswith('review.md'): continue
        assert git('rev-parse',head+':'+path)==git('rev-parse',authority+':'+path), 'frozen contract changed: '+path
    for path in ['specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md','specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md','specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md','artifacts/v10/P20/test-plan.json','artifacts/v10/P20/review.md','toolchain.lock.json']:
        assert git('rev-parse',head+':'+path)==git('rev-parse',BASE+':'+path), 'inherited authority drift: '+path
    p=json.loads(Path(FILES[0]).read_text())
    assert p['base']==BASE and len(p['services'])==8 and len(p['cases'])==18
    assert [c['id'] for c in p['cases']]==[f'P21-T{i:03}' for i in range(1,19)]
    print(json.dumps({'node':'P21','contract_status':'PASS','head':head,'authority':authority,'package_completion_claim':False}))
if __name__=='__main__': main()
