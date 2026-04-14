Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = (Resolve-Path (Join-Path $scriptDir "..\..")).Path
Set-Location $repoRoot

powershell -ExecutionPolicy Bypass -File ".\tools\generate-modules.ps1"
powershell -ExecutionPolicy Bypass -File ".\tools\generate-file-modules.ps1"

Write-Host "Updated generated files: modules + file_modules + catalogs"

