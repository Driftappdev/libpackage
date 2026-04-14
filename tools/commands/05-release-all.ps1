param(
    [ValidateSet("major","minor","patch")][string]$Bump = "patch",
    [string]$Version,
    [ValidateSet("all","standard","filemods")][string]$Scope = "all",
    [string]$Branch = "main"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = (Resolve-Path (Join-Path $scriptDir "..\..")).Path
Set-Location $repoRoot

powershell -ExecutionPolicy Bypass -File ".\tools\release-modules.ps1" `
  -Scope $Scope `
  -Bump $Bump `
  -Version $Version `
  -Branch $Branch

