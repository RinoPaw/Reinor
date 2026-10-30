#Requires -Version 5.1
<#
.SYNOPSIS
  Manually apply a staged, SHA-256-pinned Reinor Windows Agent update.
.DESCRIPTION
  Run detached (for example through the existing MCP execute_command tool).
  Leaves the current enrollment ticket and Gateway untouched. A failed startup
  restores the previous executable and attempts to reconnect with it.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidatePattern('^[a-fA-F0-9]{64}$')]
    [string]$Sha256,
    [ValidatePattern('^[a-zA-Z0-9_.-]{1,64}$')]
    [string]$Device = 'rino-pc-joined',
    [string]$Root = (Join-Path $env:LOCALAPPDATA 'RinoMesh'),
    [ValidateRange(10,180)]
    [int]$HealthTimeoutSeconds = 60,
    [ValidateRange(0,30)]
    [int]$StartupDelaySeconds = 5,
    [switch]$DryRun
)
$ErrorActionPreference = 'Stop'
$target = Join-Path $Root 'rinomesh-agent-join.exe'
$staged = Join-Path $Root 'updates\rinomesh-agent-join.exe'
$work = Join-Path $Root 'updates'
$startup = Join-Path $Root 'start-agent.ps1'
$credential = Join-Path $Root ("joined-{0}.json" -f $Device)
$taskName = 'Reinor-Agent'
$lock = Join-Path $work 'install.lock'
$log = Join-Path $work 'update-history.log'
$pending = Join-Path $Root 'rinomesh-agent-join.exe.pending'
$backup = Join-Path $Root ("rinomesh-agent-join.exe.backup-{0}-{1}" -f (Get-Date -Format 'yyyyMMdd-HHmmss'), $PID)
$failed = Join-Path $work ("failed-{0}.exe" -f $PID)
$swapped = $false
$locked = $false

function Write-Audit([string]$status) {
    Add-Content -LiteralPath $log -Encoding UTF8 -Value ("{0} device={1} status={2} sha256={3}" -f (Get-Date -Format o), $Device, $status, $Sha256.ToLowerInvariant())
}
function Assert-OrdinaryFile([string]$path) {
    $item = Get-Item -LiteralPath $path -ErrorAction Stop
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw "Refusing a directory/reparse point: $path"
    }
}
function Verify-Hash([string]$path) {
    $actual = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash
    if ($actual -ine $Sha256) { throw "SHA256 mismatch for $path" }
}
function Stop-CurrentAgent {
    $expected = [IO.Path]::GetFullPath($target)
    $processes = @(Get-CimInstance Win32_Process -Filter "Name='rinomesh-agent-join.exe'" -ErrorAction Stop |
        Where-Object { $_.ExecutablePath -and [string]::Equals(
            [IO.Path]::GetFullPath($_.ExecutablePath), $expected,
            [StringComparison]::OrdinalIgnoreCase) })
    foreach ($process in $processes) {
        Stop-Process -Id $process.ProcessId -Force -ErrorAction Stop
    }
    for ($i = 0; $i -lt 20; $i++) {
        $still = @(Get-CimInstance Win32_Process -Filter "Name='rinomesh-agent-join.exe'" |
            Where-Object { $_.ExecutablePath -and [string]::Equals(
                [IO.Path]::GetFullPath($_.ExecutablePath), $expected,
                [StringComparison]::OrdinalIgnoreCase) })
        if ($still.Count -eq 0) { return }
        Start-Sleep -Milliseconds 500
    }
    throw 'Agent process did not exit'
}
function Start-CurrentAgent {
    # Start through the existing unprivileged, enrolled-user scheduled task.
    Start-ScheduledTask -TaskName $taskName -ErrorAction Stop
}
function Wait-AgentHealthy([datetime]$notBefore) {
    $agentLog = Join-Path $Root 'join-agent-live.stderr.log'
    $expected = "connected to gateway device=$Device"
    $until = (Get-Date).AddSeconds($HealthTimeoutSeconds)
    do {
        $live = @(Get-CimInstance Win32_Process -Filter "Name='rinomesh-agent-join.exe'" -ErrorAction SilentlyContinue |
            Where-Object { $_.CommandLine -like "*join --device $Device*" })
        if ($live.Count -gt 0 -and (Test-Path -LiteralPath $agentLog)) {
            $info = Get-Item -LiteralPath $agentLog
            if ($info.LastWriteTimeUtc -ge $notBefore.ToUniversalTime()) {
                $connected = @(Get-Content -LiteralPath $agentLog -Tail 30 -ErrorAction SilentlyContinue |
                    Where-Object { $_.Contains($expected) })
                if ($connected.Count -gt 0) { return $true }
            }
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $until)
    return $false
}
function Move-WithRetry([string]$source, [string]$destination) {
    for ($attempt = 0; $attempt -lt 20; $attempt++) {
        try {
            Move-Item -LiteralPath $source -Destination $destination -ErrorAction Stop
            return
        } catch {
            if ($attempt -eq 19) { throw }
            Start-Sleep -Milliseconds 500
        }
    }
}

if (-not (Test-Path -LiteralPath $work -PathType Container)) {
    throw "Update staging directory is missing: $work"
}
foreach ($path in @($Root,$work,$target,$staged,$startup,$credential)) {
    if (-not (Test-Path -LiteralPath $path)) { throw "Required path missing: $path" }
}
Assert-OrdinaryFile $target
Assert-OrdinaryFile $staged
Assert-OrdinaryFile $credential
Assert-OrdinaryFile $startup
if ((Test-Path -LiteralPath $pending) -or (Test-Path -LiteralPath $backup)) {
    throw 'Prior update artifacts require review'
}
Verify-Hash $staged
if ($DryRun) {
    Write-Output 'VALIDATION_OK (dry-run; no processes or binaries modified)'
    return
}
try {
    New-Item -Path $lock -ItemType File -ErrorAction Stop | Out-Null
    $locked = $true
    Write-Audit 'begin'
    Copy-Item -LiteralPath $staged -Destination $pending -ErrorAction Stop
    Assert-OrdinaryFile $pending
    Verify-Hash $pending
    # Ensure the Agent can deliver the MCP acknowledgement first.
    if ($StartupDelaySeconds) { Start-Sleep -Seconds $StartupDelaySeconds }
    Stop-CurrentAgent
    Move-WithRetry $target $backup
    $swapped = $true
    Move-WithRetry $pending $target
    $since = Get-Date
    Start-CurrentAgent
    if (-not (Wait-AgentHealthy $since)) { throw 'New Agent failed Gateway health check' }
    Write-Audit 'healthy'
    Write-Output "UPDATED_OK backup=$backup"
} catch {
    $reason = $_.Exception.Message
    if ($locked) { Write-Audit 'failed' }
    if ($swapped) {
        try {
            Stop-CurrentAgent
            if (Test-Path -LiteralPath $target) {
                Move-WithRetry $target $failed
            }
            Move-WithRetry $backup $target
            $since = Get-Date
            Start-CurrentAgent
            if (Wait-AgentHealthy $since) {
                Write-Audit 'rollback_healthy'
                throw "UPDATE_FAILED_ROLLED_BACK: $reason"
            }
            Write-Audit 'rollback_unverified'
            throw "UPDATE_FAILED_RESTORED_BINARY_BUT_CONNECTION_UNVERIFIED: $reason"
        } catch {
            if ($_.Exception.Message -like 'UPDATE_FAILED_*') { throw }
            Write-Audit 'rollback_error'
            throw "UPDATE_AND_ROLLBACK_FAILED: $reason; rollback: $($_.Exception.Message)"
        }
    }
    throw
} finally {
    if (Test-Path -LiteralPath $pending) { Remove-Item -LiteralPath $pending -Force -ErrorAction SilentlyContinue }
    if ($locked) { Remove-Item -LiteralPath $lock -Force -ErrorAction SilentlyContinue }
}
