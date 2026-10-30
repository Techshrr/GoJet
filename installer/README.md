# Native Installer Boundary

GoJet V10 installation is native-only. The production web installer is implemented with PHP 8.3 FPM and exists only for the `/install/*` flow defined by the Page-Level IA.

The installer checks Nginx, PHP 8.3 FPM, MySQL 8.x, Redis, systemd, ClamAV, local-storage ownership and package integrity. Mandatory dependency failure is fail-closed and must not write the installation-complete lock.

No Docker/Compose installation option is permitted. Current implementation, recovery boundaries and remaining acceptance work are documented in [P21 status](../docs/p21/STATUS.md). The payload is not yet a production release.
