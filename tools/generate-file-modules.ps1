param(
    [string]$RootModule = "github.com/platformcore/libpackage/filemods",
    [string]$OutDir = "file_modules",
    [string]$GoVersion = "1.25.0"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$rootDir = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$outRoot = Join-Path $rootDir $OutDir

if (Test-Path $outRoot) {
    Remove-Item -LiteralPath $outRoot -Recurse -Force
}
New-Item -ItemType Directory -Path $outRoot | Out-Null

function Normalize-ModuleSegment([string]$segment) {
    $s = $segment.ToLowerInvariant()
    $s = $s -replace '[^a-z0-9._-]', '-'
    $s = $s -replace '\.+', '-'
    $s = $s -replace '-+', '-'
    $s = $s.Trim('-')
    if ([string]::IsNullOrWhiteSpace($s)) { $s = 'x' }
    return $s
}

$goFiles = Get-ChildItem -Path $rootDir -Recurse -File -Filter *.go |
    Where-Object {
        $_.FullName -notmatch '\\.gocache\\' -and
        $_.FullName -notmatch '\\file_modules\\'
    }

$items = @()

foreach ($file in $goFiles) {
    $rel = $file.FullName.Substring($rootDir.Length + 1)
    $relNoExt = [System.IO.Path]::ChangeExtension($rel, $null)

    $parts = $relNoExt -split '[\\/]'
    $moduleParts = @()
    foreach ($p in $parts) {
        $moduleParts += (Normalize-ModuleSegment $p)
    }

    $modulePath = "$RootModule/" + ($moduleParts -join '/')
    $targetDir = Join-Path $outRoot ($moduleParts -join '\\')
    New-Item -ItemType Directory -Path $targetDir -Force | Out-Null

    $targetFile = Join-Path $targetDir $file.Name
    Copy-Item -LiteralPath $file.FullName -Destination $targetFile -Force

    $goModPath = Join-Path $targetDir "go.mod"
    $goMod = @(
        "module $modulePath",
        "",
        "go $GoVersion",
        ""
    )
    [System.IO.File]::WriteAllLines($goModPath, $goMod)

    $items += [PSCustomObject]@{
        ModulePath = $modulePath
        Source     = $rel.Replace('\', '/')
        Target     = ($moduleParts -join '/')
    }
}

$items = @($items | Sort-Object ModulePath)

$installPath = Join-Path $rootDir "INSTALL_FILE_MODULES.md"
$install = New-Object System.Collections.Generic.List[string]
$install.Add("# Install Commands (Per File Module)")
$install.Add("")
$install.Add("Auto-generated: one module per .go file")
$install.Add("")
$install.Add("Total file-modules: $($items.Count)")
$install.Add("")
foreach ($i in $items) {
    $install.Add("- go get $($i.ModulePath)@latest")
}
[System.IO.File]::WriteAllLines($installPath, $install)

$catalogPath = Join-Path $rootDir "FILE_MODULE_CATALOG.md"
$catalog = New-Object System.Collections.Generic.List[string]
$catalog.Add("# File Module Catalog")
$catalog.Add("")
$catalog.Add("Auto-generated: one module per .go file")
$catalog.Add("")
$catalog.Add("count: $($items.Count)")
$catalog.Add("")
foreach ($i in $items) {
    $catalog.Add("- $($i.ModulePath)")
    $catalog.Add("  source: $($i.Source)")
    $catalog.Add("  module_dir: $OutDir/$($i.Target)")
}
[System.IO.File]::WriteAllLines($catalogPath, $catalog)

$versionsPath = Join-Path $rootDir "tools/file-module-versions.json"
$versions = [ordered]@{}
foreach ($i in $items) {
    $versions[$i.ModulePath] = "v0.1.0"
}
$versions | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $versionsPath

Write-Host "Generated per-file modules: $($items.Count)"
Write-Host "Output dir: $OutDir"
Write-Host "Updated: INSTALL_FILE_MODULES.md, FILE_MODULE_CATALOG.md, tools/file-module-versions.json"

