<?php

declare(strict_types=1);
namespace GoJet\Installer;
final class Shell
{
    /** Operable approved-plan surface; keeps the original shell and token system. */
    public static function interactive(): string
    {
        $html = self::render();
        $html = str_replace('<p class="installer-state">session-ready</p>', '', $html);
        $html = str_replace('<h1>Welcome to GoJet V10</h1>', '<h1 id="installer-title">Authorize installation</h1>', $html);
        $html = str_replace('Verify this server before starting the native installation.', 'Continue with a server-approved installation plan.', $html);
        $start = strpos($html, '<dl>');
        $end = strpos($html, '</section>', $start);
        $controls = '<p id="installer-message" role="status" aria-live="polite" aria-atomic="true">Enter the installation access token supplied by your server administrator.</p>'
            .'<form id="installer-login" method="post" action="/install/"><label for="bootstrap-token">Installation access token</label>'
            .'<input id="bootstrap-token" name="bootstrap_token" type="password" autocomplete="off" autocapitalize="none" spellcheck="false" required minlength="32" maxlength="256" aria-describedby="token-help">'
            .'<p id="token-help">Used only to authorize this installation session.</p>'
            .'<button class="installer-primary" type="submit">Authenticate</button></form>'
            .self::configurationForm()
            .'<section id="installer-preflight" aria-labelledby="preflight-title" hidden><h2 id="preflight-title">Dependency checks</h2>'
            .'<p id="preflight-summary" role="status" aria-live="polite"></p><dl id="preflight-checks" hidden></dl>'
            .'<p>These checks describe the last server observation. Installation checks dependencies again before making changes.</p></section>'
            .'<div class="installer-actions"><button id="installer-apply" class="installer-primary" type="button" hidden disabled>Submit approved installation</button>'
            .'<button id="installer-approve" type="button" hidden disabled>Confirm server approval</button>'
            .'<button id="installer-refresh" type="button">Refresh status</button></div>'
            .'<noscript><p>JavaScript is required for this protected installation interface. The server administrator can use the native installation command instead.</p></noscript>';
        $html = substr($html, 0, $start).$controls.substr($html, $end);
        $script = (string)file_get_contents(dirname(__DIR__).'/public/assets/install.js');
        return str_replace('</body>', '<script>'.$script.'</script></body>', $html);
    }
    /** Fixed field catalogue: no server secrets, filesystem paths or approval authority in HTML. */
    private static function configurationForm(): string
    {
        $groups = [
            ['Site', 'Use the final HTTPS address without a trailing slash.', [
                ['origin', 'Site HTTPS address', 'url', '', 300],
            ]],
            ['Local database and Redis', 'These services must already be configured on this server. Enter their existing credentials.', [
                ['database.port', 'MySQL port', 'number', '3306', 1, 65535],
                ['database.name', 'Database name', 'text', '', 64],
                ['database.user', 'Database user', 'text', '', 64],
                ['database.password', 'Database password', 'password', '', 1024, false],
                ['redis.port', 'Redis port', 'number', '6379', 1, 65535],
                ['redis.db', 'Redis database number', 'number', '0', 0, 15],
                ['redis.password', 'Redis password', 'password', '', 1024, false],
            ]],
            ['File storage and quotas', 'Storage paths and antivirus access are set by the server administrator. Sizes below are in bytes.', [
                ['files.max_upload_bytes', 'Maximum bytes per upload', 'number', '10485760', 1, 1072693248],
                ['files.max_files', 'Maximum files per workspace', 'number', '1000', 1, 1000000],
                ['files.max_bytes', 'Maximum stored bytes per workspace', 'number', '1073741824', 1, 9007199254740991],
                ['quotas.text', 'Text items per workspace', 'number', '1000', 1, 1000000],
                ['quotas.bio', 'Bio pages per workspace', 'number', '100', 1, 1000000],
                ['quotas.qr', 'QR codes per workspace', 'number', '1000', 1, 100000],
            ]],
            ['Email delivery', 'Use your SMTP server settings. Leave both SMTP username and password empty only when authentication is not required.', [
                ['smtp.host', 'SMTP hostname', 'text', '', 253],
                ['smtp.port', 'SMTP port', 'number', '587', 1, 65535],
                ['smtp.from', 'Sender email', 'email', '', 254],
                ['smtp.username', 'SMTP username', 'text', '', 256, false],
                ['smtp.password', 'SMTP password', 'password', '', 1024, false],
            ]],
            ['Security services', 'Supply your existing risk provider and Turnstile credentials. Internal service keys are generated privately by the server.', [
                ['risk.name', 'Risk provider name', 'text', '', 64],
                ['risk.endpoint', 'Risk provider HTTPS endpoint', 'url', '', 2048],
                ['risk.policy_version', 'Risk policy version', 'text', '', 64],
                ['turnstile_secret', 'Turnstile secret key', 'password', '', 1024],
                ['auth_rate.limit', 'Authentication attempts per window', 'number', '10', 1, 1000],
                ['auth_rate.window_seconds', 'Authentication window in seconds', 'number', '60', 1, 86400],
            ]],
            ['Initial administrator', 'This account is created only after the server validates and executes the approved plan.', [
                ['admin.email', 'Administrator email', 'email', '', 320],
                ['admin.display_name', 'Administrator display name', 'text', '', 128],
                ['admin.password', 'Administrator password', 'password', '', 1024],
            ]],
        ];
        $html = '<form id="installer-configuration" method="post" action="/install/configuration" autocomplete="off" hidden>'
            .'<p>Review all settings before saving. Saved configuration is frozen for server approval and cannot be edited here. If it needs correction, contact your server administrator.</p>';
        foreach ($groups as [$title, $help, $fields]) {
            $html .= '<fieldset><legend>'.$title.'</legend><p>'.$help.'</p>';
            foreach ($fields as $field) {
                [$path, $label, $type, $value, $limit] = $field;
                $id = 'cfg-'.str_replace('.', '-', $path);
                $optional = $type !== 'number' && ($field[5] ?? true) === false;
                $constraints = $type === 'number' ? ' min="'.$limit.'" max="'.$field[5].'" step="1"'
                    : ' maxlength="'.$limit.'"';
                $html .= '<label for="'.$id.'">'.$label.($optional ? ' (optional)' : '').'</label>'
                    .'<input id="'.$id.'" name="'.$path.'" data-config-path="'.$path.'" type="'.$type.'" value="'.$value.'"'
                    .$constraints.($optional ? '' : ' required').' autocomplete="off" autocapitalize="none" spellcheck="false">';
            }
            if ($title === 'File storage and quotas') {
                $html .= '<label for="cfg-file-types">Allowed file types</label>'
                    .'<textarea id="cfg-file-types" name="file_types" rows="3" maxlength="8192" required aria-describedby="file-types-help">pdf=application/pdf
png=image/png
jpg=image/jpeg</textarea>'
                    .'<p id="file-types-help">One extension and MIME type per line, for example pdf=application/pdf. Separate multiple MIME types with |. Only list types you intend to allow.</p>';
            }
            $html .= '</fieldset>';
        }
        return $html.'<button id="installer-save-configuration" class="installer-primary" type="submit" disabled>Save configuration for approval</button></form>';
    }
    public static function scriptPolicy(): string
    {
        return "script-src 'sha256-".base64_encode(hash('sha256', (string)file_get_contents(dirname(__DIR__).'/public/assets/install.js'), true))."'; connect-src 'self'";
    }
    public const STATES=['session-ready','step-checking','step-pass','hard-failure','retryable-failure','install-running','lock-failed','complete','already-locked'];
    public static function render(string $state='session-ready'):string
    {
        if(!in_array($state,self::STATES,true)){throw new \InvalidArgumentException('Unsupported installer shell state.');}
        $status=match($state){'hard-failure'=>['Installation blocked','A mandatory native dependency failed. Installation cannot continue.'],'retryable-failure'=>['Check failed','Correct the reported condition, then retry the current step.'],'lock-failed'=>['Installer lock failed','Do not treat the installation as complete until the lock is verified.'],'complete'=>['Installation complete','The installer is locked. Continue to Admin Login.'],'already-locked'=>['Installer unavailable','This installation has already been locked.'],'install-running'=>['Installing services','Native service configuration is in progress.'],'step-checking'=>['Checking environment','Validating required native dependencies.'],'step-pass'=>['Check passed','The current native dependency check passed.'],default=>['Welcome to GoJet V10','Verify this server before starting the native installation.']};
        $root=dirname(__DIR__,2);$tokenCss=(string)file_get_contents($root.'/frontend/packages/tokens/generated/tokens.css');$shellCss=(string)file_get_contents(dirname(__DIR__).'/public/assets/shell.css');$title=htmlspecialchars($status[0],ENT_QUOTES|ENT_SUBSTITUTE,'UTF-8');$message=htmlspecialchars($status[1],ENT_QUOTES|ENT_SUBSTITUTE,'UTF-8');$adminLink=$state==='complete'?'<a class="installer-primary" href="/admin/login">Admin Login</a>':'';
        return '<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><style>'.$tokenCss.$shellCss.'</style><title>'.$title.' · GoJet Installer</title></head><body data-shell="installer" data-state="'.$state.'"><header class="installer-header"><strong>GoJet V10 Installer</strong><span>Native installation</span></header><main class="installer-page"><ol class="installer-steps" aria-label="Installation steps"><li>Environment</li><li>Data</li><li>Site</li><li>Admin</li><li>Services</li><li>Health</li><li>Complete</li></ol><section class="installer-card"><p class="installer-state">'.htmlspecialchars($state,ENT_QUOTES,'UTF-8').'</p><h1>'.$title.'</h1><p>'.$message.'</p><dl><div><dt>Database password</dt><dd>••••••••</dd></div><div><dt>Redis credential</dt><dd>••••••••</dd></div></dl><div class="installer-actions">'.$adminLink.'<button type="button">Retry current check</button></div></section></main></body></html>';
    }
}
