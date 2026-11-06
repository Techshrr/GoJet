<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/UpgradeIO.php';

/** Quiesced local-file snapshot. No links/devices; preserves identity and never deletes failed trees. */
final class UpgradeStorage
{
    private static function inventory(string $root): array
    {
        if (realpath($root) !== $root || !is_dir($root)) { throw new \RuntimeException('Unsafe storage root'); }
        CompletionLock::verifyProvisioningPath(dirname($root).'/installed.lock');
        $rows = []; $bytes = 0;
        $walk = function(string $path, string $name) use (&$walk, &$rows, &$bytes): void {
            $s = lstat($path); $type = $s['mode'] & 0170000;
            if (!in_array($type,[0040000,0100000],true) || ($type === 0100000 && $s['nlink'] !== 1)
                || ($s['mode'] & 07000) !== 0 || count($rows) >= 100000) { throw new \RuntimeException('Unsupported storage entry'); }
            $row = ['directory'=>$type === 0040000,'uid'=>$s['uid'],'gid'=>$s['gid'],'mode'=>$s['mode'] & 0777];
            if ($type === 0100000) {
                $bytes += $s['size'];
                if ($bytes > 1073741824) { throw new \RuntimeException('Storage snapshot exceeds supported 1 GiB bound'); }
                $row += ['bytes'=>$s['size'],'sha256'=>hash_file('sha256',$path)];
            }
            $rows[$name] = $row;
            if ($type === 0040000) {
                foreach (scandir($path) as $entry) {
                    if ($entry === '.' || $entry === '..') { continue; }
                    if (preg_match('/[\x00-\x1f\x7f\\\\]/',$entry)) { throw new \RuntimeException('Unsafe storage name'); }
                    $walk($path.'/'.$entry,$name === '' ? $entry : $name.'/'.$entry);
                }
            }
        };
        $walk($root,''); ksort($rows,SORT_STRING); return $rows;
    }
    private static function copyTree(string $source, string $target, array $rows, bool $identity): void
    {
        if (file_exists($target) || is_link($target)) { throw new \RuntimeException('Storage destination exists'); }
        foreach ($rows as $name=>$row) {
            $path = $name === '' ? $target : $target.'/'.$name;
            if ($row['directory']) {
                if (!mkdir($path,0700)) { throw new \RuntimeException('Storage directory copy failed'); }
            } else {
                $mask=umask(0077);
                try { $in=fopen($source.'/'.$name,'rb'); $out=fopen($path,'xb'); } finally { umask($mask); }
                if ($in===false || $out===false) { throw new \RuntimeException('Storage file copy failed'); }
                try {
                    if (stream_copy_to_stream($in,$out)!==$row['bytes'] || !fflush($out) || !fsync($out)) { throw new \RuntimeException('Storage copy incomplete'); }
                } finally { fclose($in); fclose($out); }
                if (!hash_equals($row['sha256'],hash_file('sha256',$path))) { throw new \RuntimeException('Storage changed during copy'); }
            }
        }
        // Apply ownership last so no service can modify staging while it is built.
        foreach (array_reverse($rows,true) as $name=>$row) {
            $path=$name===''?$target:$target.'/'.$name;
            if ($identity && (!chown($path,$row['uid']) || !chgrp($path,$row['gid']) || !chmod($path,$row['mode']))) {
                throw new \RuntimeException('Storage identity restore failed');
            }
            if ($row['directory']) { UpgradeIO::sync($path); }
        }
        UpgradeIO::sync(dirname($target));
    }
    public static function backup(string $source,string $directory): string
    {
        UpgradeIO::disjoint([$source,$directory]);
        UpgradeIO::directory($directory); $rows=self::inventory($source);
        self::copyTree($source,$directory.'/files',$rows,false);
        if ($rows!==self::inventory($source)) { throw new \RuntimeException('Storage not quiescent'); }
        UpgradeIO::record($directory.'/files.json',['root'=>$source,'entries'=>$rows]);
        return hash_file('sha256',$directory.'/files.json');
    }
    public static function verify(string $directory,string $hash): array
    {
        UpgradeIO::directory($directory); $raw=UpgradeIO::read($directory.'/files.json',0600,33554432);
        if (!hash_equals($hash,hash('sha256',$raw))) { throw new \RuntimeException('Storage backup binding changed'); }
        $r=json_decode($raw,true,32,JSON_THROW_ON_ERROR); $rows=$r['entries']; $target=$r['root'];
        UpgradeIO::disjoint([$target,$directory]);
        // Check the entire backup content before moving any live directory.
        $actual=self::inventory($directory.'/files');
        if (array_keys($actual)!==array_keys($rows)) { throw new \RuntimeException('Storage backup inventory changed'); }
        foreach ($rows as $name=>$row) {
            if ($actual[$name]['directory']!==$row['directory'] || (!$row['directory'] &&
                ($actual[$name]['bytes']!==$row['bytes'] || $actual[$name]['sha256']!==$row['sha256']))) { throw new \RuntimeException('Storage backup corrupted'); }
        }
        return $r;
    }
    public static function restore(string $directory,string $hash): void
    {
        $r=self::verify($directory,$hash); $rows=$r['entries']; $target=$r['root'];
        CompletionLock::verifyProvisioningPath(dirname($target).'/installed.lock');
        $stage=dirname($target).'/.gojet-restore-'.bin2hex(random_bytes(16));
        self::copyTree($directory.'/files',$stage,$rows,true);
        if (self::inventory($stage)!==$rows) { throw new \RuntimeException('Restored storage verification failed'); }
        if (file_exists($target) || is_link($target)) {
            self::inventory($target); // Never move an unknown link/device tree.
            $failed=dirname($target).'/.gojet-preserved-'.bin2hex(random_bytes(16));
            if (!rename($target,$failed)) { throw new \RuntimeException('Cannot preserve current storage'); }
            UpgradeIO::sync(dirname($target));
        }
        if (!rename($stage,$target)) { throw new \RuntimeException('Storage restoration interrupted'); }
        UpgradeIO::sync(dirname($target));
    }
}
