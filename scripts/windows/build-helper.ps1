[CmdletBinding()]
param(
    [string]$Version = 'dev',
    [string]$OutputPath
)
$ErrorActionPreference = 'Stop'
$pulseRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
if ($Version -notmatch '^[0-9A-Za-z.+-]+$') { throw 'Invalid helper version' }
if (-not $OutputPath) { $OutputPath = Join-Path $pulseRoot '.artifacts/windows/helper/codex-pulse.exe' }
$OutputPath = [IO.Path]::GetFullPath($OutputPath)
New-Item -ItemType Directory -Path (Split-Path $OutputPath -Parent) -Force | Out-Null
Push-Location $pulseRoot
$pulsePreviousGoOS = $env:GOOS
$pulsePreviousGoArch = $env:GOARCH
try {
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    & go build -trimpath -ldflags "-X main.applicationVersion=$Version" -o $OutputPath .
    if ($LASTEXITCODE -ne 0) { throw 'Windows Helper build failed' }
} finally {
    $env:GOOS = $pulsePreviousGoOS
    $env:GOARCH = $pulsePreviousGoArch
    Pop-Location
}
