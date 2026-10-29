[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidatePattern('^[0-9A-Za-z.+-]+$')][string]$Version,
    [Parameter(Mandatory)][ValidateRange(1,[long]::MaxValue)][long]$BuildNumber,
    [ValidateSet('stable','prerelease')][string]$Channel='prerelease',
    [Parameter(Mandatory)][string]$PolicyPath,
    [string]$SigningKeyName='CodexPulse.Windows.DevelopmentUpdateSigning',
    [string]$CodeSigningCertificateThumbprint,
    [string]$OutputDirectory
)
$ErrorActionPreference='Stop'
$pulseRoot=(Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$pulsePolicy=(Resolve-Path -LiteralPath $PolicyPath).Path
$pulsePolicyValue=Get-Content -LiteralPath $pulsePolicy -Raw | ConvertFrom-Json
if(-not $pulsePolicyValue.Development -and -not $CodeSigningCertificateThumbprint){throw 'Production packaging requires an explicitly configured Authenticode certificate'}
if(-not $OutputDirectory){$OutputDirectory=Join-Path $pulseRoot ".artifacts/windows/too-673/package-$BuildNumber"}
$pulseOutput=[IO.Path]::GetFullPath($OutputDirectory)
if(Test-Path -LiteralPath $pulseOutput){throw 'Use a fresh output directory; package output is immutable'}
Push-Location $pulseRoot
try {
    & dotnet run --project app/windows/CodexPulse.PackageTools -- ensure-directory $pulseOutput
    if($LASTEXITCODE -ne 0){throw 'Private package directory initialization failed'}
    $pulseBundle=Join-Path $pulseOutput 'bundle'
    $pulseHost=Join-Path $pulseOutput 'host'
    $pulseMetadataArg="-p:PulseUpdatePolicyPath=$pulsePolicy"
    & (Join-Path $PSScriptRoot 'build-helper.ps1') -Version $Version -OutputPath (Join-Path $pulseOutput 'helper/codex-pulse.exe')
    & dotnet publish app/windows/CodexPulse.PackageHost/CodexPulse.PackageHost.csproj -c Release -r win-x64 --self-contained true $pulseMetadataArg -p:RestoreLockedMode=true -p:UseHardlinksIfPossible=false -o $pulseHost
    if($LASTEXITCODE -ne 0){throw 'Package host publication failed'}
    & dotnet publish app/windows/CodexPulse.App/CodexPulse.App.csproj -c Release -r win-x64 --self-contained true $pulseMetadataArg "-p:PulseVersion=$Version" -p:RestoreLockedMode=true -p:UseHardlinksIfPossible=false -o $pulseBundle
    if($LASTEXITCODE -ne 0){throw 'Self-contained App publication failed'}
    Copy-Item -LiteralPath (Join-Path $pulseOutput 'helper/codex-pulse.exe') -Destination (Join-Path $pulseBundle 'codex-pulse.exe')
    Copy-Item -LiteralPath (Join-Path $pulseHost 'CodexPulse.PackageHost.exe') -Destination (Join-Path $pulseBundle 'CodexPulse.PackageHost.exe')
    if($CodeSigningCertificateThumbprint){
        if($CodeSigningCertificateThumbprint -notmatch '^[0-9a-fA-F]{40}$'){throw 'Code-signing certificate thumbprint is invalid'}
        $pulseSignTool=Join-Path $env:USERPROFILE '.nuget/packages/microsoft.windows.sdk.buildtools/10.0.28000.2705/bin/10.0.28000.0/x64/signtool.exe'
        $pulseBinaries=@(Get-ChildItem -LiteralPath $pulseBundle -File | Where-Object {$_.Name -like 'CodexPulse.*.exe' -or $_.Name -like 'CodexPulse.*.dll' -or $_.Name -eq 'codex-pulse.exe'})
        foreach($pulseBinary in $pulseBinaries){
            & $pulseSignTool sign /s My /sha1 $CodeSigningCertificateThumbprint /fd SHA256 $pulseBinary.FullName
            if($LASTEXITCODE -ne 0){throw 'Authenticode signing failed'}
            & $pulseSignTool verify /pa $pulseBinary.FullName
            if($LASTEXITCODE -ne 0){throw 'Authenticode trust verification failed'}
        }
    }
    & dotnet run --project app/windows/CodexPulse.PackageTools -- pack $pulseBundle $pulseOutput $Version $BuildNumber $Channel $pulsePolicy $SigningKeyName
    if($LASTEXITCODE -ne 0){throw 'Paired signed bundle creation failed'}
    Copy-Item -LiteralPath (Join-Path $pulseBundle 'CodexPulse.PackageHost.exe') -Destination (Join-Path $pulseOutput 'Setup.exe')
    Get-FileHash -LiteralPath (Join-Path $pulseOutput 'Setup.exe'),(Join-Path $pulseOutput "codex-pulse-$BuildNumber-win-x64.zip"),(Join-Path $pulseOutput 'release.json') -Algorithm SHA256 |
        Select-Object @{Name='Name';Expression={[IO.Path]::GetFileName($_.Path)}},Hash | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $pulseOutput 'SHA256.json') -Encoding utf8
    Write-Output "Windows package ready: $pulseOutput"
}finally{Pop-Location}
