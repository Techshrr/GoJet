---
name: Linketic
description: A restrained, task-first interface for managing links and shared content.
colors:
  primary: "#2855df"
  surface: "#ffffff"
  canvas: "#f7f8fb"
  text-primary: "#17223b"
  text-secondary: "#55627a"
  divider: "#e3e7ef"
typography:
  display:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "clamp(36px, 4.5vw, 64px)"
    fontWeight: 650
    lineHeight: 1.06
    letterSpacing: "-.035em"
  product-title:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "30px"
    lineHeight: 1.2
    letterSpacing: "-.025em"
  auth-title:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "29px"
    lineHeight: 1.15
    letterSpacing: "-.03em"
  body:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "14px"
    lineHeight: 1.55
  label:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "12px"
    fontWeight: 600
  brand:
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
    fontSize: "26px"
    fontWeight: 750
    letterSpacing: "-.04em"
rounded:
  md: "8px"
  lg: "12px"
  preview: "14px"
  xl: "16px"
spacing:
  "1": "4px"
  "2": "8px"
  "3": "12px"
  "4": "16px"
  "5": "20px"
  "6": "24px"
  "8": "32px"
  "10": "40px"
  "12": "48px"
  "16": "64px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.surface}"
    rounded: "{rounded.md}"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.md}"
  button-outline:
    backgroundColor: "transparent"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.md}"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.md}"
  button-destructive:
    backgroundColor: "#B91C1C"
    textColor: "{colors.surface}"
    rounded: "{rounded.md}"
  button-link:
    backgroundColor: "transparent"
    textColor: "#234cce"
    rounded: "{rounded.md}"
  website-primary-link:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.surface}"
    rounded: "{rounded.md}"
    padding: "11px 20px"
  auth-input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.md}"
    height: "46px"
  sidebar-link:
    textColor: "#52617b"
    rounded: "7px"
    padding: "9px 14px"
  status-chip:
    backgroundColor: "#f1f3f7"
    textColor: "#5b687d"
    rounded: "5px"
    padding: "4px 9px"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.lg}"
    padding: "{spacing.4}"
---

# Design System: Linketic

## Overview

**Creative North Star: "Task-first clarity"**

Linketic uses a restrained white, slate and blue interface. Its product identity comes from readable typography, clear actions, quiet boundaries and consistent navigation. Preserve the Linketic name and supplied identity assets; the implementation choices recorded here do not replace trademark assets.

Working surfaces are denser than the public website. Authentication concentrates attention on one form, while administration and customer workspaces use persistent navigation and structured records. Product illustrations support the website and authentication layout; they are explicitly labeled as example content.

**Key Characteristics:**

- White working surfaces on a cool, pale canvas.
- Self-hosted Inter with compact product typography and larger website headlines.
- Blue primary actions and selected navigation.
- Rounded controls, thin boundaries and selective soft shadows.
- Explicit state labels and responsive records with visible actions.

This is a source-derived record of commit `7893ce0d2aab4d2bd4abab71b415bb9853ab4fc7`. Sources include `PRODUCT.md`, the product stylesheet, shared UI styles and components, generated tokens, website/authentication shells, and administration governance screens. It documents the implemented layer; it is not a claim of screenshot approval, complete accessibility verification or complete redesign of secondary screens.

The product stylesheet at `frontend/packages/ui/src/brand/product.css` overrides selected inherited tokens and adds surface-specific rules. Shared controls still inherit other values from `frontend/packages/tokens/generated/tokens.css` and `frontend/packages/ui/src/styles.css`. The frontmatter records the recurring product primitives; the sidecar records component states and extensions.

## Colors

The palette combines a clear blue action color with cool neutrals. It has one brand accent; semantic feedback colors communicate state.

### Primary

- **Action Blue** (`primary`): primary actions and recurring identity accents.
- Text links use the product layer's darker link blue. Selected sidebar destinations use a pale blue fill and darker blue text.
- Website primary links have their own darker hover color. Shared UI buttons retain their existing token-based hover and pressed colors; these are not identical implementations.

### Neutral

- **White Surface** (`surface`): cards, forms, headers and navigation.
- **Cool Canvas** (`canvas`): workspace and administration backgrounds.
- **Slate Ink** (`text-primary`): primary content.
- **Supporting Slate** (`text-secondary`): secondary content.
- **Quiet Divider** (`divider`): inherited shared component boundaries.

Authentication uses a slightly different pale canvas. Component-specific border and secondary-text shades remain in the source; they are not a new universal palette.

Semantic feedback retains the shared success, warning, danger and information tokens. Management status chips use their own subdued green, amber and neutral treatments. Always pair color with readable status text.

**The State Label Rule.** Color supports a written status; it never replaces the status label.

## Typography

**Display and Body Font:** Inter, self-hosted as a variable WOFF2 font with weights from 100 to 900 and `font-display: swap`. The product stack includes sans-serif fallbacks.

The same family connects website, authentication and working surfaces. Hierarchy comes from size, weight, spacing and line length rather than ornamental type changes. The brand wordmark treatment is compact and heavy; this is a text treatment, not permission to redraw supplied logos.

### Hierarchy

- **Display:** the frontmatter display role describes the website hero. Its line length is constrained to approximately 13 characters on desktop. On narrow screens the implemented hero uses 44px and a 14-character measure.
- **Product title:** administration and workspace page headings use the product-title role.
- **Authentication title:** the auth-title role reduces to 26px at the product phone breakpoint.
- **Body:** the body role is the shared product default. Website introductory copy uses 17px with a 1.75 line height, reducing to 16px on narrow screens.
- **Labels and metadata:** compact labels identify fields and columns. Body content should retain its own hierarchy rather than inheriting illustration-sized text.
- **Verification input:** the six-digit field uses 26px, weight 650, tabular numbers and `.48em` letter spacing. This is a single input with enlarged tracking, not six independent fields.

Some illustration and metadata text in the source is below 12px. That is an implementation detail requiring legibility review, not a recommended type size for new functional controls or explanatory text.

## Layout

The product shells use a two-column grid: a 240px sidebar and a flexible content column with `minmax(0, 1fr)`. Content has a 1440px maximum width and 36px desktop padding. At 1000px, content padding reduces to 24px. At 760px, sidebars disappear and the content becomes one column with 24px vertical and 16px horizontal padding. Headers reduce from a 76px minimum height to 64px.

Authentication uses a two-column form-and-brand layout with a shared topbar and footer. The form card has a 490px maximum width and 40px padding. At 1000px its padding becomes 30px. At 760px the layout becomes a vertical stack, the illustrative brand column is hidden, and the card uses a 460px maximum width with 28px vertical and 24px horizontal padding.

The public website uses a 1240px page container. The homepage pairs text with an illustrative product panel; its columns are `1fr` and `1.1fr`, separated by 48px. The website has separate adjustments at 900px and 700px. At 700px the homepage stacks, the illustration loses its rotation and the three-column supporting content becomes a single column.

Management tables are bounded by a rounded, scroll-capable container. At 760px, records become two-column blocks with full-width identity and action areas. Column labels remain available as field labels; account actions remain visible. Detail records reduce from two columns to one.

The spacing frontmatter preserves the inherited four-pixel scale used throughout the project. Existing product surfaces also contain context-specific values such as 18px, 26px and 36px. Do not rewrite those values merely to force mathematical uniformity.

**The Task Hierarchy Rule.** Page titles, the current task and its actions establish the hierarchy before supplemental description or illustration.

## Elevation & Depth

Working surfaces are primarily separated by background tone and thin borders. Authentication and welcome cards use a soft ambient shadow; the website illustration uses a broader shadow. Shared cards are flat by default and have an explicit elevated variant.

### Shadow Vocabulary

- **Authentication and welcome:** `0 12px 45px #17223b08`.
- **Website illustration:** `0 24px 70px #23385d0d`.
- **Selected authentication mode:** `0 2px 6px #182c4f0a`.
- **Shared elevated card:** `0 1px 2px rgba(15,23,42,.05)`.
- **Shared popovers and tooltips:** `0 8px 24px rgba(15,23,42,.08)`.
- **Shared dialogs:** `0 20px 60px rgba(15,23,42,.12)`.

**The Selective Elevation Rule.** Use borders and surface tone for ordinary working groups; reserve existing shadow treatments for their established elevated or illustrative roles.

## Shapes

Controls use the medium radius, management containers use the large radius, illustrations use the preview radius, and authentication cards use the extra-large radius. The source also uses a 7px sidebar-link radius, 5px status-chip radius and circular account avatars.

The slightly rotated website and authentication previews are illustration treatments. They are not a pattern for working forms, data tables or navigation.

Borders are generally one pixel. Focus indicators and semantic warning boundaries are state treatments with their own rules.

## Components

### Buttons

The shared UI button supports primary, secondary, outline, ghost, destructive and link variants. Its default variant is secondary. It uses a medium radius, 12px inline padding and an inherited minimum hit height of 40px. Button states include hover, pressed, selected, disabled and loading.

Website primary links use a 44px minimum height and their own padding and hover treatment. Authentication buttons use a 46px minimum height, weight 600 and a short background transition. These contexts are related, but they are not interchangeable sizing contracts.

Shared button transitions use 160ms with `cubic-bezier(.2,0,0,1)`. Authentication background transitions use 150ms ease. The product reduced-motion rule disables transitions and smooth scrolling. Shared controls retain a small pressed translation; reduced-motion behavior should be checked in the actual cascade before changes.

### Inputs / Fields

Plain product fields have white backgrounds, thin cool borders, medium corners and a 44px minimum height. Authentication inputs are 46px tall and use 15px text. Read-only authentication fields use a pale fill and muted text.

The enlarged verification input is 60px tall. Keep its associated label, format guidance and resend feedback visible. State notices use text with a semantic SVG icon and appropriate status or alert semantics.

Focus currently has two implementations:

- Plain product controls receive a 3px pale-blue outline with a 3px offset.
- Shared UI controls have a more specific token-based 2px outline, 2px offset and white backplate. Authentication input focus also has a specific border and translucent outline rule.

This is recorded cascade debt, not a requirement to create additional focus styles. Preserve visible focus and reconcile these treatments through a separately reviewed change.

### Navigation

Product sidebars use white surfaces, slate links, pale hover fills and a blue selected treatment. Product logo links remain visually distinct from selected destinations.

The website provides a focus-only skip link to the main content and a native modal navigation sheet on compact layouts. Its control has an explicit accessible name, expanded state and dialog relationship. Localized navigation labels describe destinations.

Website sign-in and workspace actions reflect the server session. Keep the visual emphasis aligned with the action actually available to that user.

### Chips

Management status chips are compact, softly rounded labels. Active, pending and other statuses have distinct fills and readable text. They are informational labels, not implicitly clickable controls.

### Cards / Containers

Shared cards are white, bordered and flat by default, with large corners and 16px padding. Metric cards increase padding to 24px on desktop and 20px on phones. Authentication cards use more space and softer elevation.

Management details use a bordered white container, while access-change confirmations use a distinct warning boundary. Do not erase that distinction when reusing the card system.

### Responsive Management Records

Desktop rows align identity, status, verification, membership, date and actions. Phone records keep the same information in labeled blocks. Names and emails wrap rather than forcing the whole page wider. Access actions remain separate from identity/detail actions.

Show loading, no records, no matching results and error feedback explicitly. The visible count describes the loaded set; it must not imply a total beyond the returned records.

### Product Illustrations

Illustrative previews use small SVG link arrows, example URLs and explicit example-content captions. They are supporting visuals, not live workspace data or operational controls. Their reduced text sizes are not reusable product typography.

## Do's and Don'ts

### Do:

- **Do** preserve Linketic branding and supplied identity assets.
- **Do** use the shared primitives while respecting existing surface-specific sizing.
- **Do** pair status colors with readable text.
- **Do** keep keyboard focus visible and navigation controls explicitly labeled.
- **Do** preserve identity, field labels and access actions when records reflow on phones.
- **Do** label illustrative product data as example content.
- **Do** distinguish source inspection, automated tests and visual acceptance in delivery evidence.

### Don't:

- **Don't** introduce decorative headings above every task title.
- **Don't** replace SVG interface icons with decorative text glyphs.
- **Don't** turn illustration-sized text into a functional text standard.
- **Don't** apply preview rotation or ambient shadows indiscriminately to working surfaces.
- **Don't** treat the current light product layer as proof that dark mode is visually complete.
- **Don't** use this document to imply that secondary screens, localized typography or all interaction states have been visually approved.
