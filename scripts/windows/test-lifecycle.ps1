[CmdletBinding()]
param()
$ErrorActionPreference='Stop'
$pulseRoot=(Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$pulseOutput=Join-Path $pulseRoot '.artifacts/windows/too-671'
$pulseFixture=Join-Path $pulseOutput ('lifecycle-'+[Guid]::NewGuid().ToString('N')+'-中文 path')
New-Item -ItemType Directory -Path $pulseOutput -Force | Out-Null
Push-Location $pulseRoot
try {
    & go build -o (Join-Path $pulseOutput 'codex-pulse.exe') .
    if($LASTEXITCODE -ne 0){throw 'Helper build failed'}
    & go build -o (Join-Path $pulseOutput 'seed.exe') ./scripts/windows/transport-fixture
    if($LASTEXITCODE -ne 0){throw 'Fixture build failed'}
    foreach($pulseName in @('normal','crash')){
        & (Join-Path $pulseOutput 'seed.exe') (Join-Path $pulseFixture $pulseName) normal
        if($LASTEXITCODE -ne 0){throw 'Fixture initialization failed'}
    }
    & dotnet run --project app/windows/CodexPulse.Tests --no-restore -p:RestoreLockedMode=true -- --lifecycle (Join-Path $pulseOutput 'codex-pulse.exe') $pulseFixture
    if($LASTEXITCODE -ne 0){throw 'Windows lifecycle process tests failed'}
}finally{Pop-Location}
