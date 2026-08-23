<?php

declare(strict_types=1);

namespace GoJet\Installer;

final class FilePreflight
{
    private const MAX_OUTPUT_BYTES = 65536;
    private const TIMEOUT_SECONDS = 10.0;

    /** @return array{state:string,report:?array<string,mixed>} */
    public static function check(): array
    {
        $binary = trim((string) getenv('GOJET_FILE_PREFLIGHT_BIN'));
        if ($binary === '' || $binary[0] !== '/' || !is_file($binary) || !is_executable($binary)) {
            return self::hardFailure();
        }

        $descriptors = [
            1 => ['pipe', 'w'],
            2 => ['pipe', 'w'],
        ];
        $process = @proc_open([$binary], $descriptors, $pipes, null, null, ['bypass_shell' => true]);
        if (!is_resource($process)) {
            return self::hardFailure();
        }

        stream_set_blocking($pipes[1], false);
        stream_set_blocking($pipes[2], false);
        $stdout = '';
        $deadline = microtime(true) + self::TIMEOUT_SECONDS;
        $status = proc_get_status($process);

        while ($status['running'] && microtime(true) < $deadline) {
            $stdout .= (string) stream_get_contents($pipes[1], self::MAX_OUTPUT_BYTES - strlen($stdout));
            // Drain stderr without surfacing private dependency details to the installer UI.
            stream_get_contents($pipes[2]);
            if (strlen($stdout) >= self::MAX_OUTPUT_BYTES) {
                proc_terminate($process);
                self::closeProcess($process, $pipes);
                return self::hardFailure();
            }
            usleep(20000);
            $status = proc_get_status($process);
        }

        if ($status['running']) {
            proc_terminate($process);
            self::closeProcess($process, $pipes);
            return self::hardFailure();
        }

        $stdout .= (string) stream_get_contents($pipes[1], self::MAX_OUTPUT_BYTES - strlen($stdout));
        stream_get_contents($pipes[2]);
        $exitCode = (int) $status['exitcode'];
        foreach ($pipes as $pipe) {
            fclose($pipe);
        }
        $closedExitCode = proc_close($process);
        if ($exitCode < 0) {
            $exitCode = $closedExitCode;
        }
        if ($exitCode !== 0 || strlen($stdout) >= self::MAX_OUTPUT_BYTES) {
            return self::hardFailure();
        }

        try {
            $report = json_decode($stdout, true, 32, JSON_THROW_ON_ERROR);
        } catch (\JsonException) {
            return self::hardFailure();
        }
        if (!is_array($report) || !self::isHealthyReport($report)) {
            return self::hardFailure();
        }

        return ['state' => 'step-pass', 'report' => $report];
    }

    /** @param array<string,mixed> $report */
    private static function isHealthyReport(array $report): bool
    {
        return ($report['ready'] ?? null) === true
            && ($report['status'] ?? null) === 'healthy'
            && is_array($report['storage'] ?? null)
            && ($report['storage']['state'] ?? null) === 'healthy'
            && ($report['storage']['writable'] ?? null) === true
            && is_array($report['clamav'] ?? null)
            && ($report['clamav']['state'] ?? null) === 'healthy'
            && is_string($report['clamav']['engine_version'] ?? null)
            && $report['clamav']['engine_version'] !== ''
            && is_string($report['clamav']['signature_version'] ?? null)
            && $report['clamav']['signature_version'] !== '';
    }

    /** @param array<int,resource> $pipes */
    private static function closeProcess($process, array $pipes): void
    {
        foreach ($pipes as $pipe) {
            if (is_resource($pipe)) {
                fclose($pipe);
            }
        }
        proc_close($process);
    }

    /** @return array{state:string,report:null} */
    private static function hardFailure(): array
    {
        return ['state' => 'hard-failure', 'report' => null];
    }
}
