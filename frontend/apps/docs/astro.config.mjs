import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://gojet.cc',
  base: '/docs',
  integrations: [
    starlight({
      title: 'GoJet Docs',
      defaultLocale: 'en',
      locales: {
        en: { label: 'English', lang: 'en' },
        'zh-CN': { label: '简体中文', lang: 'zh-CN' },
      },
      sidebar: [
        { label: 'Start', items: [{ label: 'Overview', slug: 'en' }] },
      ],
      customCss: ['./src/styles/docs-shell.css'],
      components: {
        SocialIcons: './src/components/WorkspaceLink.astro',
      },
    }),
  ],
});
