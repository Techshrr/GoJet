<?php

declare(strict_types=1);
namespace GoJet\Installer;
final class Shell
{
    public const STATES=['session-ready','step-checking','step-pass','hard-failure','retryable-failure','install-running','lock-failed','complete','already-locked'];
    public static function render(string $state='session-ready'):string
    {
        if(!in_array($state,self::STATES,true)){throw new \InvalidArgumentException('Unsupported installer shell state.');}
        $status=match($state){'hard-failure'=>['Installation blocked','A mandatory native dependency failed. Installation cannot continue.'],'retryable-failure'=>['Check failed','Correct the reported condition, then retry the current step.'],'lock-failed'=>['Installer lock failed','Do not treat the installation as complete until the lock is verified.'],'complete'=>['Installation complete','The installer is locked. Continue to Admin Login.'],'already-locked'=>['Installer unavailable','This installation has already been locked.'],'install-running'=>['Installing services','Native service configuration is in progress.'],'step-checking'=>['Checking environment','Validating required native dependencies.'],'step-pass'=>['Check passed','The current native dependency check passed.'],default=>['Welcome to GoJet V10','Verify this server before starting the native installation.']};
        $root=dirname(__DIR__,2);$tokenCss=(string)file_get_contents($root.'/frontend/packages/tokens/generated/tokens.css');$shellCss=(string)file_get_contents(dirname(__DIR__).'/public/assets/shell.css');$title=htmlspecialchars($status[0],ENT_QUOTES|ENT_SUBSTITUTE,'UTF-8');$message=htmlspecialchars($status[1],ENT_QUOTES|ENT_SUBSTITUTE,'UTF-8');$adminLink=$state==='complete'?'<a class="installer-primary" href="/admin/login">Admin Login</a>':'';
        return '<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><style>'.$tokenCss.$shellCss.'</style><title>'.$title.' · GoJet Installer</title></head><body data-shell="installer" data-state="'.$state.'"><header class="installer-header"><strong>GoJet V10 Installer</strong><span>Native installation</span></header><main class="installer-page"><ol class="installer-steps" aria-label="Installation steps"><li>Environment</li><li>Data</li><li>Site</li><li>Admin</li><li>Services</li><li>Health</li><li>Complete</li></ol><section class="installer-card"><p class="installer-state">'.htmlspecialchars($state,ENT_QUOTES,'UTF-8').'</p><h1>'.$title.'</h1><p>'.$message.'</p><dl><div><dt>Database password</dt><dd>••••••••</dd></div><div><dt>Redis credential</dt><dd>••••••••</dd></div></dl><div class="installer-actions">'.$adminLink.'<button type="button">Retry current check</button></div></section></main></body></html>';
    }
}
