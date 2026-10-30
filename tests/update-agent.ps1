$ErrorActionPreference = 'Stop'
$script = Join-Path (Split-Path $PSScriptRoot -Parent) 'deploy\update-agent.ps1'
$root = Join-Path ([IO.Path]::GetTempPath()) ('reinor-updater-tests-' + [guid]::NewGuid().ToString('N'))
try {
    New-Item -ItemType Directory -Path (Join-Path $root 'updates') -Force | Out-Null
    $target = Join-Path $root 'rinomesh-agent-join.exe'
    $stage = Join-Path $root 'updates\rinomesh-agent-join.exe'
    $original = [Text.Encoding]::ASCII.GetBytes('currently-installed-agent')
    $candidate = [Text.Encoding]::ASCII.GetBytes('new-version-agent')
    [IO.File]::WriteAllBytes($target, $original)
    [IO.File]::WriteAllBytes($stage, $candidate)
    [IO.File]::WriteAllText((Join-Path $root 'start-agent.ps1'), 'exit 0')
    [IO.File]::WriteAllText((Join-Path $root 'joined-rino-pc-joined.json'), '{}')
    $sha = (Get-FileHash -LiteralPath $stage -Algorithm SHA256).Hash
    $out = & $script -Root $root -Sha256 $sha -Device 'rino-pc-joined' -DryRun
    if ("$out" -notmatch 'VALIDATION_OK') { throw 'Valid staging package was rejected' }
    $failureObserved = $false
    try {
        & $script -Root $root -Sha256 ('0' * 64) -Device 'rino-pc-joined' -DryRun | Out-Null
    } catch { $failureObserved = $_.Exception.Message -like '*SHA256 mismatch*' }
    if (-not $failureObserved) { throw 'Digest mismatch was not rejected' }
    if ([Convert]::ToBase64String([IO.File]::ReadAllBytes($target)) -ne
        [Convert]::ToBase64String($original)) { throw 'Dry-run modified active binary' }
    if (Test-Path (Join-Path $root 'updates\install.lock')) {
        throw 'Dry-run created an installation lock'
    }
    Write-Output 'UPDATE_AGENT_STAGE_TESTS_OK'
} finally {
    if (Test-Path $root) { Remove-Item -LiteralPath $root -Recurse -Force }
}
