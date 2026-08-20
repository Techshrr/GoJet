# Native Installer Boundary

GoJet V10 installation is native-only. The production web installer is implemented with PHP 8.3 FPM and exists only for the `/install/*` flow defined by the Page-Level IA.

The installer must eventually preflight Nginx, PHP 8.3 FPM, MySQL 8.x, Redis, systemd, ClamAV, local-storage ownership and package integrity. Mandatory dependency failure is fail-closed and must not write the installation-complete lock.

No Docker/Compose installation option is permitted. P00 establishes the boundary; P21/P22 own release/fresh-install completion.
