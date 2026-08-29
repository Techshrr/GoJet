import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://gojet.cc',
  base: '/docs',
  trailingSlash: 'never',
  integrations: [
    starlight({
      title: 'GoJet Docs',
      defaultLocale: 'en',
      locales: {
        en: { label: 'English', lang: 'en' },
        'zh-CN': { label: '简体中文', lang: 'zh-CN' },
      },
      sidebar: [
        {
          label: 'Start',
          translations: { 'zh-CN': '开始' },
          items: [
            { label: 'Overview', translations: { 'zh-CN': '概览' }, slug: 'index' },
            { label: 'Getting started', translations: { 'zh-CN': '开始使用' }, slug: 'getting-started' },
            { label: 'Native self-hosting', translations: { 'zh-CN': '原生自托管' }, slug: 'self-hosting' },
          ],
        },
        {
          label: 'API',
          translations: { 'zh-CN': 'API' },
          items: [
            { label: 'API reference', translations: { 'zh-CN': 'API 参考' }, slug: 'api' },
            { label: 'API keys', translations: { 'zh-CN': 'API Keys' }, slug: 'api/api-keys' },
            { label: 'Webhooks', translations: { 'zh-CN': 'Webhooks' }, slug: 'api/webhooks' },
          ],
        },
      ],
      customCss: ['./src/styles/docs-shell.css'],
      components: {
        Head: './src/components/Head.astro',
        SocialIcons: './src/components/WorkspaceLink.astro',
      },
    }),
  ],
});
