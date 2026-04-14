param(
    [string]$Branch = "main",
    [switch]$WithTags
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = (Resolve-Path (Join-Path $scriptDir "..\..")).Path
Set-Location $repoRoot

git push origin $Branch
if ($WithTags) {
    git push --tags
}

