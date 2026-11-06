# P21 — Native Release Package

Status: **ENGINEERING COVERAGE REVIEW COMPLETE — ACCOUNTABLE G11 APPROVAL PENDING**

Candidate `10.0.0-rc.1`, source `32ea29d766b0ac0790964517ce4e220a1208bf0d`.
Archive SHA-256: `7e7343c34571f69038e090f20775e1a941b5a33b80825e755813dd08500f1542`.

The missing OS/CPU/capacity preflight is implemented through native execution, completion and upgrade health, with the ninth check displayed in desktop/mobile Installer views. The initial packaging omission is corrected; archive assembly and read-back reject missing PHP includes.

[Native payload run 42161906247](https://github.com/Techshrr/GoJet/actions/runs/42161906247) passed all steps: 16 Python tests, native compilation/Go regression, four frontend builds, six host failures, continuous installation, eight services, MySQL/configuration recovery, 209 risk links, ten incompatible candidates and fifteen interruption/recovery stages. Actual archive bytes were independently downloaded and verified; the candidate index reproduces exactly.

[Dependency run 42164219962](https://github.com/Techshrr/GoJet/actions/runs/42164219962), test source `53b1c61dc5491ef9d2c5ed1052b43a8860b9d33b`, passed real PHP 8.1 CLI/FPM and MariaDB 10.11 refusals against this exact archive. All other eight checks stay healthy, original readiness and authority are preserved, and the downloaded report hash was verified.

- [Current immutable candidate and original report bindings](host-preflight/candidate.json)
- [Archive/download/CI validation](host-preflight/validation.json)
- [Current-archive dependency versions](host-preflight/dependency-versions/validation.json)
- [Component reports and unchanged-source bindings](host-preflight/components/index.json)
- [Eighteen-case engineering review](../../../docs/p21/contract-coverage.md)
- [G11 evidence and accountable review checklist](../gates/G11/native-package/review-checklist.json)
- [Original attempt and correction](host-preflight/review.json)

The engineering coverage review is complete; this is not eighteen accountable approvals. G11 remains pending with Release Engineer. G1 belongs to Platform Lead; P22 roles retain their specified responsibilities. No signature or approval is fabricated.

Original reports retain their component-scope false flags. The same-binary upgrade fixture does not certify arbitrary historical releases; the concrete dependency matrix is not exhaustive version certification. P21 runbook static validation is complete; clean-host execution, the specified Ubuntu 24.04.4 target/reboot and real external-provider/production acceptance remain P22 work. CI used Ubuntu 24.04.5. The package remains installable=false, no production deployment or permanent formal release is claimed, and the current archive expires 2026-12-06.

[Previous review](history/review-before-host-preflight.txt) and [previous G11 index](history/g11-before-host-preflight/evidence-index.json) preserve earlier evidence without occupying the implementation mainline.
