param(
    [string]$RootModule = "github.com/driftappdev",
    [string]$GoVersion = "1.25.0",
    [string]$VersionsFile = "tools/module-versions.json",
    [string]$DefaultVersion = "v0.1.0"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$rootDir = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

function ConvertTo-Hashtable($jsonObject) {
    $table = @{}
    if ($null -eq $jsonObject) { return $table }
    foreach ($p in $jsonObject.PSObject.Properties) {
        $table[$p.Name] = [string]$p.Value
    }
    return $table
}

function Get-RelativePath([string]$FromPath, [string]$ToPath) {
    $fromUri = New-Object System.Uri(($FromPath.TrimEnd('\') + '\'))
    $toUri = New-Object System.Uri(($ToPath.TrimEnd('\') + '\'))
    $relativeUri = $fromUri.MakeRelativeUri($toUri).ToString()
    $relativePath = [System.Uri]::UnescapeDataString($relativeUri).Replace('/', '/')
    if ([string]::IsNullOrWhiteSpace($relativePath)) { return "." }
    return $relativePath.TrimEnd('/')
}

function Get-ModuleItems {
    param([string]$RepoRoot)

    $mods = @()
    Get-ChildItem -Path $RepoRoot -Recurse -File -Filter go.mod |
        Where-Object { $_.FullName -notmatch "\\.gocache\\" } |
        ForEach-Object {
            $first = (Get-Content -LiteralPath $_.FullName -TotalCount 1).Trim()
            if (-not $first.StartsWith("module ")) {
                throw "Invalid go.mod (missing module line): $($_.FullName)"
            }
            $modulePath = $first.Substring(7).Trim()
            $dir = Split-Path -Parent $_.FullName
            $relDir = Get-RelativePath -FromPath $RepoRoot -ToPath $dir
            if ([string]::IsNullOrWhiteSpace($relDir) -or $relDir -eq ".") {
                $relDir = "."
            }

            $mods += [PSCustomObject]@{
                ModulePath = $modulePath
                RelDir     = $relDir
                GoModPath  = $_.FullName
            }
        }

    $dup = $mods | Group-Object ModulePath | Where-Object { $_.Count -gt 1 }
    if ($dup) {
        $names = ($dup | ForEach-Object { $_.Name }) -join ", "
        throw "Duplicate module path(s): $names"
    }

    return @($mods | Sort-Object ModulePath)
}

$modules = Get-ModuleItems -RepoRoot $rootDir

# Generate INSTALL_MODULES.md
$installPath = Join-Path $rootDir "INSTALL_MODULES.md"
$install = New-Object System.Collections.Generic.List[string]
$install.Add("# Install Commands")
$install.Add("")
$install.Add("Auto-generated from all go.mod files in this repository.")
$install.Add("")
$install.Add("Total modules: $($modules.Count)")
$install.Add("")
foreach ($m in $modules) {
    $install.Add("- go get $($m.ModulePath)@latest")
}
$install.Add("")
$install.Add("Tip (module-specific version tag):")
$install.Add("- Root module uses tag: vX.Y.Z")
$install.Add("- Submodule uses tag: <subdir>/vX.Y.Z (example: core/result/v1.2.0)")
$install.Add("")
[System.IO.File]::WriteAllLines($installPath, $install)

# Generate MODULE_CATALOG.md
$catalogPath = Join-Path $rootDir "MODULE_CATALOG.md"
$catalog = New-Object System.Collections.Generic.List[string]
$catalog.Add("# Module Catalog")
$catalog.Add("")
$catalog.Add("Auto-generated from all go.mod files in this repository.")
$catalog.Add("")
$catalog.Add("count: $($modules.Count)")
$catalog.Add("")
foreach ($m in $modules) {
    $catalog.Add("- $($m.ModulePath) ($($m.RelDir))")
    $catalog.Add("  install: go get $($m.ModulePath)@latest")
}
$catalog.Add("")
$catalog.Add("Workspace:")
$catalog.Add("- run: go work sync")
[System.IO.File]::WriteAllLines($catalogPath, $catalog)

# Generate / update versions file for per-module releases
$versionsPath = Join-Path $rootDir $VersionsFile
$versionsObj = [ordered]@{}

if (Test-Path $versionsPath) {
    try {
        $existingRaw = Get-Content -Raw -LiteralPath $versionsPath | ConvertFrom-Json
        $existing = ConvertTo-Hashtable $existingRaw
        foreach ($k in $existing.Keys) {
            $versionsObj[$k] = [string]$existing[$k]
        }
    } catch {
        throw "Invalid JSON in $versionsPath : $($_.Exception.Message)"
    }
}

foreach ($m in $modules) {
    if (-not $versionsObj.Contains($m.ModulePath)) {
        $versionsObj[$m.ModulePath] = $DefaultVersion
    }
}

# Remove stale entries for modules that no longer exist
$currentSet = @{}
foreach ($m in $modules) { $currentSet[$m.ModulePath] = $true }
foreach ($k in @($versionsObj.Keys)) {
    if (-not $currentSet.ContainsKey($k)) {
        $versionsObj.Remove($k)
    }
}

$orderedOut = [ordered]@{}
foreach ($k in ($versionsObj.Keys | Sort-Object)) {
    $orderedOut[$k] = $versionsObj[$k]
}

$versionsDir = Split-Path -Parent $versionsPath
if (-not (Test-Path $versionsDir)) {
    New-Item -ItemType Directory -Path $versionsDir | Out-Null
}
$orderedOut | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $versionsPath

Write-Host "Generated: INSTALL_MODULES.md, MODULE_CATALOG.md"
Write-Host "Updated versions: $VersionsFile"
Write-Host "Total modules: $($modules.Count)"

