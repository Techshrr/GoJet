<?php
declare(strict_types=1);
require_once __DIR__.'/../../../installer/src/HostPreflight.php';
use GoJet\Installer\HostPreflight;
function expect(bool $condition): void { if (!$condition) { throw new RuntimeException('Host boundary failed'); } }
$valid = "NAME=Ubuntu\nID=ubuntu\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04.5 LTS\"\n";
expect(HostPreflight::supported($valid, 'Linux', 'x86_64', 8));
foreach (['', "ID=debian\nVERSION_ID=24.04\n", "ID=ubuntu\nVERSION_ID=22.04\n",
    "ID=ubuntu\nVERSION_ID=24.04.5\n", $valid."ID=ubuntu\n", $valid."VERSION_ID=24.04\n",
    "ID=ubuntu\nVERSION_ID=\$(echo 24.04)\n", str_repeat('x', 16385)] as $invalid) {
    expect(!HostPreflight::supported($invalid, 'Linux', 'x86_64', 8));
}
foreach ([['FreeBSD', 'x86_64', 8], ['Linux', 'aarch64', 8], ['Linux', 'i686', 8], ['Linux', 'x86_64', 4]] as $platform) {
    expect(!HostPreflight::supported($valid, ...$platform));
}
echo "Host identity boundaries passed (unit scope; no native install claim).\n";
