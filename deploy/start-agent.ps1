param([string]$device = $env:COMPUTERNAME.ToLowerInvariant())
$ErrorActionPreference='Stop'
$root=Join-Path $env:LOCALAPPDATA 'Reinor'
$exe=Join-Path $root 'reinor-agent.exe'
$credential=Join-Path $root ("joined-$device.json")
if((-not(Test-Path $exe)) -or (-not(Test-Path $credential))){exit 2}
$existing=Get-Process -Name 'reinor-agent' -ErrorAction SilentlyContinue
if($existing){exit 0}
# Only the child process inherits this environment. Other applications keep their proxy settings.
foreach($k in @('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','http_proxy','https_proxy','all_proxy')) {
  [Environment]::SetEnvironmentVariable($k,$null,'Process')
}
$env:NO_PROXY='xuhuaai.top'
$env:no_proxy='xuhuaai.top'
Start-Process -FilePath $exe -ArgumentList @('join','--device',$device) -WorkingDirectory $root -WindowStyle Hidden -RedirectStandardError (Join-Path $root 'join-agent-live.stderr.log') -RedirectStandardOutput (Join-Path $root 'join-agent-live.stdout.log') | Out-Null
'REINOR_AGENT_START_ISSUED'
