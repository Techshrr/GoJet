# Linketic

Linketic is the public brand of the existing GoJet repository. The application manages short links, QR codes, files, text, bio pages, workspaces and associated customer/admin accounts. Do not replace existing business rules, data, permission checks or known brand assets as part of visual work.

## Current delivery goal

Repair registration, email verification, email sign-in and password recovery, keep authenticated navigation working across the website and customer workspace, and deliver a coherent customer/admin interface with browser evidence. Users must not type opaque security tokens or guess the next screen.

- Email entry uses six numeric digits, with expiry feedback and resend.
- Password recovery stays on one page.
- Successful authentication establishes a server session and opens the customer workspace.
- Existing accounts, credentials, mail history and application data survive supported upgrades.
- Available social login providers are configured by administrators; unconfigured buttons are hidden.
- Fresh installations open a web wizard at the domain. Docker is optional; a systemd edition needs one privileged bootstrap before the web wizard can run.

## Audience and use

Customers create/manage links and related content on desktop and phones. Administrators need readable lists, account status, useful details and explicit confirmation for access changes. Installation is performed by a site owner using a hosting panel; repeated shell repair sequences are unacceptable.

## Visual commitment

Use the conventional product clarity and task flow of the user's Short.io and s.ee references. Preserve Linketic branding. Build a restrained, readable interface with consistent controls, typography, spacing and responsive navigation. The current implementation uses a self-hosted Inter face and a white/slate/blue palette; these are implementation choices, not a replacement for any supplied trademark assets.

## Evidence boundaries

An installation completion marker proves installation checks, not all business functionality. Protocol fixtures prove integration behavior, not real recipient inbox delivery or live external OAuth authorization. Label example marketing preview content. Never present untested compatibility or an unfinished release as production-ready.
