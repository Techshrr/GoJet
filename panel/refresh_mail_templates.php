<?php
declare(strict_types=1);
// Upgrade only unchanged shipped defaults; customized templates remain untouched.
if (posix_geteuid()!==0) exit(1);
$root=json_decode(file_get_contents('/var/lib/linketic/private/mysql.json'),true,8,JSON_THROW_ON_ERROR);
$db=new PDO('mysql:unix_socket=/run/mysqld/mysqld.sock;dbname=gojet;charset=utf8mb4','root',$root['password'],[PDO::ATTR_ERRMODE=>PDO::ERRMODE_EXCEPTION]);
$defaults=[
 'auth-email-verification'=>['verification_code','Verify your GoJet email','Use this verification code to finish creating your GoJet account:','The code expires shortly. If you did not request this account, ignore this message.','Verify your Linketic email','Enter this six-digit code on the verification page to finish creating your account.','15 minutes'],
 'auth-login-email-code'=>['login_code','Your GoJet login code','Use this one-time code to sign in to GoJet:','The code expires shortly. If you did not request it, ignore this message.','Your Linketic sign-in code','Enter this six-digit code on the sign-in page.','10 minutes'],
 'auth-password-reset'=>['reset_token','Reset your GoJet password','Use this one-time reset token to reset your GoJet password:','The token expires shortly. If you did not request it, ignore this message.','Reset your Linketic password','Return to the password reset page. Enter this six-digit code and your new password on the same page.','30 minutes'],
];
$db->beginTransaction();
foreach($defaults as $key=>[$variable,$oldSubject,$oldIntro,$oldFooter,$subject,$intro,$expiry]) {
 $q=$db->prepare('SELECT * FROM mail_templates WHERE template_key=? AND locale=\'en\' ORDER BY version DESC LIMIT 1 FOR UPDATE');$q->execute([$key]);$old=$q->fetch(PDO::FETCH_ASSOC);
 $placeholder='{{'.$variable.'}}';
 if(!$old || (int)$old['version']!==1 || !$old['enabled'] || $old['subject_template']!==$oldSubject || $old['text_template']!==$oldIntro.' '.$placeholder."\n\n".$oldFooter || $old['html_template']!=='<p>'.$oldIntro.'</p><p><strong>'.$placeholder.'</strong></p><p>'.$oldFooter.'</p>') continue;
 $footer='This code expires in '.$expiry.'. Request a new code from the same page if needed. If you did not request this email, you can ignore it.';
 $html='<div style="font-family:Arial,sans-serif;max-width:520px;margin:32px auto;color:#162341"><p style="font-size:20px;font-weight:700">Linketic</p><h1 style="font-size:24px">'.$subject.'</h1><p style="line-height:1.6">'.$intro.'</p><p style="font-size:34px;letter-spacing:8px;font-weight:700;padding:24px;background:#f2f5fb;border-radius:8px">'.$placeholder.'</p><p style="font-size:13px;line-height:1.7;color:#5a6880">'.$footer.'</p></div>';
 $q=$db->prepare('INSERT INTO mail_templates(template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only,enabled) VALUES (?,\'en\',2,?,?,?,?,0,1)');
 $q->execute([$key,$subject,$intro."\n\n".$placeholder."\n\n".$footer,$html,json_encode([$variable],JSON_THROW_ON_ERROR)]);
}
$db->commit();
