import json
from pathlib import Path
import tempfile
import unittest
from build_payload import validate_static
class StaticPayloadTests(unittest.TestCase):
    def test_localized_docs_not_spa(self):
        with tempfile.TemporaryDirectory() as d:
            repo=Path(d); dist=repo/'dist'; manifest=repo/'frontend/apps/docs/src/data/content-manifest.json'
            manifest.parent.mkdir(parents=True)
            manifest.write_text(json.dumps({'documents':[{'canonicalPath':'/docs/en'},{'canonicalPath':'/docs/zh-CN'},{'canonicalPath':'/docs/en/api'}]}))
            for name in ['404.html','en/search/index.html','zh-CN/search/index.html','pagefind/pagefind.js','en/index.html','zh-CN/index.html','en/api/index.html']:
                path=dist/name; path.parent.mkdir(parents=True,exist_ok=True); path.write_text('content')
            validate_static(dist,'docs',repo)
            self.assertFalse((dist/'index.html').exists())
            (dist/'zh-CN/index.html').unlink()
            with self.assertRaisesRegex(ValueError,'zh-CN/index.html'): validate_static(dist,'docs',repo)
    def test_spa_requires_entry(self):
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(ValueError): validate_static(Path(d),'workspace')
