/**
 * P02 machine-readable projection of the approved GoJet V10 brand contract.
 *
 * IMPORTANT: this file intentionally stores token NAMES and asset identifiers,
 * not exact visual values. Exact values remain authoritative only in
 * GJ-V10-DS-GREENFIELD-2026-08-20. P03 may generate implementation tokens from
 * that specification; P02 must not create a second value authority.
 */
export const BRAND_FOUNDATION = {
  authority: {
    documentId: 'GJ-V10-DS-GREENFIELD-2026-08-20',
    masterId: 'GJ-V10-MP-GREENFIELD-2026-08-20',
    implementationStage: 'P02',
  },
  assets: [
    'logo-full-light.svg',
    'logo-full-dark.svg',
    'logo-mark.svg',
    'favicon.svg',
    'favicon.ico',
    'apple-touch-icon.png',
    'og-brand.png',
  ],
  logoTokens: {
    websiteHeight: 'asset.logo.website.height',
    productHeight: 'asset.logo.product.height',
    safeArea: 'asset.logo.safe-area',
  },
  iconTokens: {
    inline: 'icon.size.inline',
    navigation: 'icon.size.navigation',
    marketing: 'icon.size.marketing',
    empty: 'icon.size.empty',
    defaultStroke: 'icon.stroke.default',
    smallStroke: 'icon.stroke.small',
  },
  brandColorTokens: [
    'color.blue.600',
    'color.cyan.500',
    'color.sky.400',
    'color.slate.950',
    'color.slate.50',
  ],
  gradientTokens: [
    'gradient.hero-ambient',
    'gradient.brand-border',
    'gradient.data-highlight',
  ],
  jetPath: {
    elements: ['path', 'node', 'split', 'pulse'],
    allowedContexts: [
      'hero',
      'product-transition',
      'routing',
      'a-b',
      'analytics',
      'empty-illustration',
      'loading-illustration',
    ],
    prohibitedContexts: ['input-border', 'button', 'table-cell', 'every-card'],
    motionTokens: {
      pathDuration: 'motion.duration.path',
      reducedDuration: 'motion.duration.reduced',
    },
  },
  functionalIconSource: 'Lucide',
  externalBrandIconSourceOrder: ['Official Brand Kit', 'Official SVG', 'Simple Icons'],
  attributionRegistry: 'frontend/packages/tokens/brand/BRAND-ASSET-LICENSES.md',
} as const;

export type BrandFoundation = typeof BRAND_FOUNDATION;
