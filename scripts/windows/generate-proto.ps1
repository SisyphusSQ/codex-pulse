[CmdletBinding()]
param([switch]$Check)
$ErrorActionPreference = 'Stop'
$pulseRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$pulseProject = Join-Path $pulseRoot 'app/windows/CodexPulse.Protocol'
$pulseGenerated = Join-Path $pulseProject 'obj/Debug/net10.0/api/codexpulse/core/v1'
Push-Location (Join-Path $pulseRoot 'app/windows')
try {
    if ($Check -and -not (Test-Path -LiteralPath (Join-Path $pulseGenerated 'Core.cs'))) {
        throw 'Build the protocol project before checking reproducibility'
    }
    $pulseBefore = @{}
    if ($Check) {
        foreach ($pulseName in @('Core.cs', 'CoreGrpc.cs')) {
            $pulseBefore[$pulseName] = (Get-FileHash -LiteralPath (Join-Path $pulseGenerated $pulseName)).Hash
        }
    }
    & dotnet build CodexPulse.Protocol/CodexPulse.Protocol.csproj -t:Rebuild -p:RestoreLockedMode=true
    if ($LASTEXITCODE -ne 0) { throw 'C# protocol generation failed' }
    if ($Check) {
        foreach ($pulseName in @('Core.cs', 'CoreGrpc.cs')) {
            if ((Get-FileHash -LiteralPath (Join-Path $pulseGenerated $pulseName)).Hash -ne $pulseBefore[$pulseName]) {
                throw "C# protocol generation is not reproducible: $pulseName"
            }
        }
        Write-Output 'C# protocol generation is reproducible'
    }
} finally { Pop-Location }
