import unittest
from t033_case import inspect_locales, normalized_doc

HEAD = 'a' * 40


def fixture():
    # Synthetic admission fixture, not runtime evidence.
    en = ['/'] + [f'/page-{i}' for i in range(25)]
    zh = ['/zh-CN/' if p == '/' else '/zh-CN' + p for p in en]
    graph = {p: [p] for p in en + zh}
    docs = [f'/docs/en/page-{i}' for i in range(6)] + [f'/docs/zh-CN/page-{i}' for i in range(7)]
    canonical = [{'path': p} for p in docs]
    articles = [{'path': p, 'internal_links': [p]} for p in [docs[0], docs[1], docs[-1]]]
    searches = {}
    for locale, case, field in [('en', 'P18-T008', 'eligible_published_english'), ('zh-CN', 'P18-T009', 'eligible_published_zh_cn')]:
        eligible = [p for p in docs if p.split('/')[2] == locale]
        searches[case] = {'implementation_commit': HEAD, 'status': 'PASS', 'case': case, field: eligible,
                         'pagefind_bundle': True, 'locale_canonical_preserved': True,
                         'probe': {'state': 'results', 'offline': False, 'pagefind_requests': 2,
                                   'external_requests': [], 'hrefs': [eligible[0] + '/']}}
    return [graph, canonical, docs.copy(), articles, searches, HEAD]


class LocaleTests(unittest.TestCase):
    def test_complete_locale_fixture(self):
        result = inspect_locales(*fixture())
        self.assertEqual(result['docs_locales']['en']['published'], 6)
        self.assertEqual(result['docs_locales']['zh-CN']['published'], 7)

    def test_rejects_invented_mixed_or_missing_targets(self):
        for mode in ['peer', 'cross-locale', 'published', 'docs-navigation', 'search-target', 'foreign-head', 'duplicate-index', 'external']:
            with self.subTest(mode=mode):
                data = fixture()
                if mode == 'peer': data[0]['/zh-CN/fake'] = data[0].pop('/zh-CN/page-0')
                elif mode == 'cross-locale': data[0]['/'] = ['/zh-CN/']
                elif mode == 'published': data[2][-1] = '/docs/zh-CN/unpublished'
                elif mode == 'docs-navigation': data[3][0]['internal_links'] = ['/docs/zh-CN/page-0']
                elif mode == 'foreign-head': data[4]['P18-T008']['implementation_commit'] = 'b' * 40
                elif mode == 'duplicate-index': data[4]['P18-T008']['eligible_published_english'].append('/docs/en/page-0')
                else: data[4]['P18-T008']['probe']['hrefs'] = ['https://elsewhere.test/docs/en/page-0' if mode == 'external' else '/docs/en/unpublished']
                with self.assertRaises(ValueError): inspect_locales(*data)

    def test_doc_home_alias_and_fragment(self):
        self.assertEqual(normalized_doc('/docs/en/#intro'), '/docs/en')
        with self.assertRaises(ValueError): normalized_doc('//elsewhere.test/docs/en/')


if __name__ == '__main__':
    unittest.main()
