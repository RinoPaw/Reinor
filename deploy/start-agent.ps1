# RinoMesh Agent autostart launcher for the signed-in Windows user.
# Install shortcut to this script in the user's Startup folder.
$ErrorActionPreference = 'Stop'
$root = Join-Path $env:LOCALAPPDATA 'RinoMesh'
$exe = Join-Path $root 'rinomesh-agent.exe'
$token = Join-Path $root 'agent.token'
if ((-not (Test-Path $exe)) -or (-not (Test-Path $token))) { exit 1 }
if (Get-Process -Name 'rinomesh-agent' -ErrorAction SilentlyContinue) { exit 0 }
$args = @('-device','rino-pc','-url','wss://xuhuaai.top/agent/ws','-token-file', ('"' + $token + '"'))
Start-Process -FilePath $exe -ArgumentList $args -WorkingDirectory $root -WindowStyle Hidden -RedirectStandardError (Join-Path $root 'go-agent.err.log') -RedirectStandardOutput (Join-Path $root 'go-agent.out.log') | Out-Null
