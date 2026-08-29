import { defineCollection, z } from 'astro:content';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';

const metaDocs = z.object({
  locale: z.enum(['en', 'zh-CN']),
  canonicalPath: z.string().regex(/^\/docs\/(?:en|zh-CN)(?:\/|\/[-a-z0-9/]+)?$/),
  translation: z.string().regex(/^\/docs\/(?:en|zh-CN)(?:\/|\/[-a-z0-9/]+)?$/).nullable(),
  contentOwner: z.string().min(1),
});

export const collections = {
  docs: defineCollection({
    loader: docsLoader(),
    schema: docsSchema({ extend: metaDocs }),
  }),
};
