# P21 — Native Release Package

Status: **PENDING — COMPLETE CONTRACT COVERAGE AND G11 REVIEW**

Candidate `10.0.0-rc.1`, source `270e8459f2862525fd8e75db7a375b4346da4dcf`.
Archive SHA-256: `e8abdc99ea137f41ba0e5df8a3ac0bf7fe38f65acfc9de24b0d3fd51f013e8d9`.

The native install/upgrade/recovery implementation and four packaged operator runbooks are delivered.
[Native payload run 42001339781](https://github.com/Techshrr/GoJet/actions/runs/42001339781)
succeeded on its first attempt, including eight services, actual MySQL recovery, 209 risk links,
ten incompatible-candidate refusals and fifteen interruption/recovery stages.
The downloaded artifact was independently verified; the candidate index reproduces exactly.

- [Immutable candidate and eight original report bindings](candidate/candidate.json)
- [Full package inventory](candidate/manifest.json)
- [Independent download verification](candidate/download-verification.json)
- [Run/job/artifact bindings and limitations](candidate/validation.json)
- [G11 evidence index](../gates/G11/native-package/evidence-index.json)
- [Eighteen-case review navigation](../gates/G11/native-package/review-checklist.json)
- [Packaged Chinese runbooks](../../../docs/release/README.md)

Remaining P21 work is complete contract coverage review and G11 acceptance.
The previously missing concrete version-refusal evidence is now supplied by
[native dependency run 42075233647](https://github.com/Techshrr/GoJet/actions/runs/42075233647)
on test source `a49023029aedcc2c915dbdf8d0f54d34d400c79b`, using this exact unchanged archive.
Real PHP 8.1 CLI/FPM and MariaDB 10.11 are refused with the other seven checks healthy;
original readiness, FPM identity and installation authority are preserved. The artifact was
independently downloaded and verified. See [the bound report](dependency-versions/validation.json).
This is a concrete incompatibility matrix, not exhaustive version certification.
Automated results are not eighteen case approvals. Preserve original component-scope false flags and evidence
provenance. The same-binary transition fixture does not certify arbitrary historical releases.
No requirement for two prior formal releases is added.

The master plan assigns G11 to **Release Engineer**, and the G1 architecture review to
**Platform Lead**. QA and Security Reviewer belong to the relevant P22/G12/G13 roles;
they are not an invented four-party G11 approval prerequisite. Reviewer identity and immutable
source/archive/evidence bindings must be explicit; no role signature is inferred here.

P22 owns the specified clean-host/reboot and owner-controlled production validation.
CI ran Ubuntu 24.04.5, not the requested 24.04.4 target. The package remains installable=false;
no production deployment or G11 approval is claimed. The CI archive expires 2026-12-05 and has
not been published as a permanent formal Release. Historical PR progress is preserved in
[the separate checkpoint](history/pr-207-before-candidate-delivery.md).
