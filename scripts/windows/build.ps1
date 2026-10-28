[CmdletBinding()]
param(
    [ValidateSet('Debug', 'Release')][string]$Configuration = 'Debug',
    [switch]$LockedRestore,
    [string]$Version = 'dev'
)
$ErrorActionPreference = 'Stop'
$pulseRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
& (Join-Path $PSScriptRoot 'build-helper.ps1') -Version $Version
Push-Location (Join-Path $pulseRoot 'app/windows')
try {
    $pulseRestoreArgs = @('restore', 'CodexPulse.App/CodexPulse.App.csproj')
    if ($LockedRestore) { $pulseRestoreArgs += '--locked-mode' }
    & dotnet @pulseRestoreArgs
    if ($LASTEXITCODE -ne 0) { throw 'Windows dependency restore failed' }
    & dotnet build CodexPulse.App/CodexPulse.App.csproj --no-restore -c $Configuration
    if ($LASTEXITCODE -ne 0) { throw 'Windows app build failed' }
} finally { Pop-Location }
