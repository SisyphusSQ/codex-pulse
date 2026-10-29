[CmdletBinding()]
param()
$ErrorActionPreference='Stop'
$pulseRoot=(Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$pulseOutput=Join-Path $pulseRoot '.artifacts/windows/too-670'
$pulseFixture=Join-Path $pulseOutput ('controls-'+[Guid]::NewGuid().ToString('N')+'-中文 path')
New-Item -ItemType Directory -Path $pulseOutput -Force | Out-Null
Push-Location $pulseRoot
try {
    & go build -o (Join-Path $pulseOutput 'codex-pulse.exe') .
    if($LASTEXITCODE -ne 0){throw 'Helper build failed'}
    & go build -o (Join-Path $pulseOutput 'seed.exe') ./scripts/windows/transport-fixture
    if($LASTEXITCODE -ne 0){throw 'Fixture build failed'}
    & (Join-Path $pulseOutput 'seed.exe') (Join-Path $pulseFixture 'normal') normal
    if($LASTEXITCODE -ne 0){throw 'Normal fixture initialization failed'}
    & (Join-Path $pulseOutput 'seed.exe') (Join-Path $pulseFixture 'recovery') recovery-restore
    if($LASTEXITCODE -ne 0){throw 'Recovery fixture initialization failed'}
    & dotnet run --project app/windows/CodexPulse.Tests -- --controls (Join-Path $pulseOutput 'codex-pulse.exe') $pulseFixture
    if($LASTEXITCODE -ne 0){throw 'Windows control contract tests failed'}
}finally{Pop-Location}
