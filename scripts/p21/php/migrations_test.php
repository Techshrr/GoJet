<?php
declare(strict_types=1);
require dirname(__DIR__, 3).'/installer/src/Migrations.php';
use GoJet\Installer\Migrations;
function need(bool $ok, string $message): void { if (!$ok) { throw new RuntimeException($message); } }
function rejected(callable $f, string $expected): void {
    try { $f(); } catch (RuntimeException $e) { need(str_contains($e->getMessage(), $expected), 'wrong rejection: '.$e->getMessage()); return; }
    throw new RuntimeException('expected rejection: '.$expected);
}
$parsed = Migrations::statements("-- comment\nSELECT ';', 'a''b';\nDELIMITER $$\nCREATE TRIGGER t BEFORE UPDATE ON x FOR EACH ROW BEGIN SIGNAL SQLSTATE '45000'; END$$\nDELIMITER ;\n");
need(count($parsed) === 2 && str_contains($parsed[1], "'45000'; END"), 'trigger boundary lost');
rejected(fn() => Migrations::statements("SELECT 'unterminated;"), 'incomplete');
rejected(fn() => Migrations::statements('SELECT 1'), 'incomplete');
$root = getenv('P21_MIGRATION_ROOT'); $head = getenv('P21_SOURCE');
$hash = hash_file('sha256', $root.'/migration-catalog.json');
$db = new PDO(getenv('P21_TEST_DSN'), 'gojet_test', getenv('P21_TEST_PASSWORD'), [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION, PDO::ATTR_EMULATE_PREPARES => false]);
$r = Migrations::apply($db, $root, $hash, $head, '10.0.0-rc.1');
need($r['total'] === 31 && $r['applied_now'] === 31, 'full immutable catalog not applied');
need(Migrations::apply($db, $root, $hash, $head, '10.0.0-rc.1')['applied_now'] === 0, 'rerun not idempotent');
need((int)$db->query("SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME LIKE 'trg_admin_domain_entitlement_decisions_%'")->fetchColumn() === 2, 'DELIMITER triggers missing');
$other = new PDO(getenv('P21_TEST_DSN'), 'gojet_test', getenv('P21_TEST_PASSWORD'));
$database = $db->query('SELECT DATABASE()')->fetchColumn(); $lock = 'gojet-migrate-'.substr(hash('sha256', $database), 0, 40);
$s = $other->prepare('SELECT GET_LOCK(?,0)'); $s->execute([$lock]); need((int)$s->fetchColumn() === 1, 'test lock acquisition');
rejected(fn() => Migrations::apply($db, $root, $hash, $head, '10.0.0-rc.1'), 'lock busy');
$s = $other->prepare('SELECT RELEASE_LOCK(?)'); $s->execute([$lock]);
$db->exec("UPDATE gojet_schema_migrations SET checksum=REPEAT('0',64) WHERE id=1");
rejected(fn() => Migrations::apply($db, $root, $hash, $head, '10.0.0-rc.1'), 'history requires');
$db->exec("UPDATE gojet_schema_migrations SET checksum='".Migrations::load($root, $hash)[0]['sha256']."',status='started' WHERE id=1");
rejected(fn() => Migrations::apply($db, $root, $hash, $head, '10.0.0-rc.1'), 'history requires');
$fixture = sys_get_temp_dir().'/gojet-migrations-'.bin2hex(random_bytes(8));
mkdir($fixture.'/migrations', 0700, true);
$sql = 'CREATE TABLE partial_effect(id INT PRIMARY KEY); INVALID MIGRATION STATEMENT;';
file_put_contents($fixture.'/migrations/000001_partial.sql', $sql);
$fixtureCatalog = ['schema' => 'gojet.migration-catalog.v1', 'migrations' => [['id' => 1, 'path' => 'migrations/000001_partial.sql', 'bytes' => strlen($sql), 'sha256' => hash('sha256', $sql), 'rollback' => 'restore-verified-database-backup; no automatic down migration']]];
file_put_contents($fixture.'/migration-catalog.json', json_encode($fixtureCatalog));
$fixtureHash = hash_file('sha256', $fixture.'/migration-catalog.json');
$partial = new PDO(getenv('P21_PARTIAL_DSN'), 'gojet_test', getenv('P21_TEST_PASSWORD'), [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
rejected(fn() => Migrations::apply($partial, $fixture, $fixtureHash, $head, '10.0.0-rc.1'), 'verified database restore');
need($partial->query("SELECT status FROM gojet_schema_migrations WHERE id=1")->fetchColumn() === 'failed', 'partial failure journal missing');
need($partial->query("SHOW TABLES LIKE 'partial_effect'")->fetchColumn() === 'partial_effect', 'test did not reach committed DDL');
rejected(fn() => Migrations::apply($partial, $fixture, $fixtureHash, $head, '10.0.0-rc.1'), 'history requires');
file_put_contents($fixture.'/migrations/000001_partial.sql', 'SELECT 1;');
rejected(fn() => Migrations::load($fixture, $fixtureHash), 'integrity mismatch');
unlink($fixture.'/migrations/000001_partial.sql'); unlink($fixture.'/migration-catalog.json'); rmdir($fixture.'/migrations'); rmdir($fixture);
echo json_encode(['native_mysql' => true, 'full_catalog' => 31, 'delimiter_triggers' => 2, 'rerun_noop' => true, 'lock_rejected' => true, 'drift_rejected' => true, 'interrupted_journal_rejected' => true, 'real_partial_ddl_rejected_on_retry' => true, 'original_bytes_tamper_rejected' => true], JSON_THROW_ON_ERROR)."\n";
