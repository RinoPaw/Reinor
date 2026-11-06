# Build the Windows 7 compatible Reinor Agent using the Go 1.20.14 toolchain.
# This builds only the Agent. The Gateway keeps its current Go toolchain.
[CmdletBinding()]
param(
    [ValidateSet("all", "amd64", "386")]
    [string]$Architecture = "all",
    [string]$GoExecutable = "$env:USERPROFILE\go\bin\go1.20.14.exe"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
$oldGOAMD64 = $env:GOAMD64
try {
    $version = & $GoExecutable version
    if ($LASTEXITCODE -ne 0 -or $version -notmatch "go1\.20\.14") {
        throw "Go 1.20.14 is required for Win7. Found: $version"
    }

    & $GoExecutable test "-modfile=go.win7.mod" ./cmd/agent ./internal/wire
    if ($LASTEXITCODE -ne 0) { throw "Agent tests failed" }
    & $GoExecutable vet "-modfile=go.win7.mod" ./cmd/agent ./internal/wire
    if ($LASTEXITCODE -ne 0) { throw "Agent vet failed" }

    New-Item -ItemType Directory -Path "dist" -Force | Out-Null
    $env:GOOS = "windows"
    $env:CGO_ENABLED = "0"
    $env:GOAMD64 = "v1"
    $architectures = if ($Architecture -eq "all") { @("amd64", "386") } else { @($Architecture) }
    foreach ($arch in $architectures) {
        $env:GOARCH = $arch
        $output = Join-Path "dist" "reinor-agent-win7-$arch.exe"
        & $GoExecutable build -trimpath "-modfile=go.win7.mod" -o $output ./cmd/agent
        if ($LASTEXITCODE -ne 0) { throw "Agent build failed ($arch)" }
        $file = Get-Item $output
        $sha256 = (Get-FileHash $file.FullName -Algorithm SHA256).Hash
        Write-Output "$($file.FullName) [$($file.Length) bytes] SHA256=$sha256"
    }
}
finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    $env:CGO_ENABLED = $oldCGO
    $env:GOAMD64 = $oldGOAMD64
    Pop-Location
}
