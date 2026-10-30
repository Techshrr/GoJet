<?php
declare(strict_types=1);
namespace GoJet\Installer;
require_once __DIR__.'/UpgradeIO.php';

/** Discard only GoJet risk projections; retain durable queues and unrelated keys. */
final class UpgradeRedis
{
    private static function reply($socket,int $depth=0): mixed
    {
        $line=fgets($socket,1024);
        if ($depth>2 || $line===false || !str_ends_with($line,"\r\n")) { throw new \RuntimeException('Redis recovery reply invalid'); }
        $type=$line[0]; $value=substr($line,1,-2);
        if ($type==='+') { return $value; }
        if (!preg_match('/^[0-9]{1,8}$/D',$value)) { throw new \RuntimeException('Redis recovery command refused'); }
        $n=(int)$value;
        if ($type===':') { return $n; }
        if ($type==='$' && $n<=65536) {
            $raw='';
            while (strlen($raw)<$n+2) {
                $part=fread($socket,$n+2-strlen($raw));
                if ($part===false || $part==='') { throw new \RuntimeException('Redis recovery read failed'); }
                $raw.=$part;
            }
            if (!str_ends_with($raw,"\r\n")) { throw new \RuntimeException('Redis recovery framing invalid'); }
            return substr($raw,0,-2);
        }
        if ($type==='*' && $n<=10000) {
            $rows=[];
            for ($i=0;$i<$n;++$i) { $rows[]=self::reply($socket,$depth+1); }
            return $rows;
        }
        throw new \RuntimeException('Redis recovery reply exceeded bounds');
    }
    private static function send($socket,array $args): mixed
    {
        $raw='*'.count($args)."\r\n";
        foreach ($args as $arg) { $raw.='$'.strlen($arg)."\r\n".$arg."\r\n"; }
        if (fwrite($socket,$raw)!==strlen($raw)) { throw new \RuntimeException('Redis recovery write failed'); }
        return self::reply($socket);
    }
    public static function invalidate(array $r): int
    {
        UpgradeIO::root();
        if (!is_int($r['port']??null) || $r['port']<1 || $r['port']>65535 || !is_int($r['db']??null)
            || $r['db']<0 || $r['db']>15 || !is_string($r['password']??null) || strlen($r['password'])>1024) {
            throw new \RuntimeException('Invalid recovery Redis configuration');
        }
        $socket=@stream_socket_client('tcp://127.0.0.1:'.$r['port'],$errno,$error,3);
        if ($socket===false) { throw new \RuntimeException('Recovery Redis unavailable'); }
        stream_set_timeout($socket,3); $deleted=0; $visited=0; $deadline=microtime(true)+60;
        try {
            if (($r['password']!=='' && self::send($socket,['AUTH',$r['password']])!=='OK')
                || self::send($socket,['SELECT',(string)$r['db']])!=='OK') { throw new \RuntimeException('Recovery Redis selection failed'); }
            foreach (['risk:link:*','risk:bio-child:*'] as $pattern) {
                $cursor='0';
                do {
                    if (microtime(true)>$deadline || $visited>100000) { throw new \RuntimeException('Risk cache recovery exceeded bounds'); }
                    $result=self::send($socket,['SCAN',$cursor,'MATCH',$pattern,'COUNT','500']);
                    if (!is_array($result) || count($result)!==2 || !is_string($result[0])
                        || !preg_match('/^[0-9]+$/D',$result[0]) || !is_array($result[1])) { throw new \RuntimeException('Invalid risk cache inventory'); }
                    [$cursor,$keys]=$result; $visited+=count($keys);
                    foreach ($keys as $key) {
                        if (!is_string($key) || !preg_match('/^risk:(?:link|bio-child):[1-9][0-9]*:[a-f0-9]{64}$/D',$key)) {
                            throw new \RuntimeException('Unexpected risk cache key');
                        }
                    }
                    foreach (array_chunk($keys,100) as $batch) {
                        $count=self::send($socket,['DEL',...$batch]);
                        if (!is_int($count)) { throw new \RuntimeException('Risk cache invalidation failed'); }
                        $deleted+=$count;
                    }
                } while ($cursor!=='0');
            }
            return $deleted;
        } finally { fclose($socket); }
    }
}
