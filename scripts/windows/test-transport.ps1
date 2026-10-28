[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$pulseRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$pulseOutput = Join-Path $pulseRoot '.artifacts/windows/too-661'
$pulseFixture = Join-Path $pulseOutput ('fixture-' + [Guid]::NewGuid().ToString('N') + '-中文 path')
New-Item -ItemType Directory -Path $pulseOutput -Force | Out-Null
Push-Location $pulseRoot
try {
    & go build -o (Join-Path $pulseOutput 'codex-pulse.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'Helper build failed' }
    & go build -o (Join-Path $pulseOutput 'seed.exe') ./scripts/windows/transport-fixture
    if ($LASTEXITCODE -ne 0) { throw 'Fixture build failed' }
    foreach ($pulseMode in @('normal', 'recovery')) {
        & (Join-Path $pulseOutput 'seed.exe') (Join-Path $pulseFixture $pulseMode) $pulseMode
        if ($LASTEXITCODE -ne 0) { throw 'Fixture initialization failed' }
    }
    & dotnet run --project app/windows/CodexPulse.Tests -- --transport (Join-Path $pulseOutput 'codex-pulse.exe') $pulseFixture
    if ($LASTEXITCODE -ne 0) { throw 'C# Go transport tests failed' }
    & go test -c -o (Join-Path $pulseOutput 'wire-peer.exe') ./internal/helper
    if ($LASTEXITCODE -ne 0) { throw 'Wire peer build failed' }
    & dotnet run --project app/windows/CodexPulse.Tests -- --wire-peer (Join-Path $pulseOutput 'wire-peer.exe') (Join-Path $pulseFixture 'wire')
    if ($LASTEXITCODE -ne 0) { throw 'C# Go wire boundary tests failed' }
} finally { Pop-Location }
