param(
    [string]$VersionsFile = "tools/module-versions.json",
    [string]$Module,
    [string]$Version,
    [ValidateSet("major","minor","patch")][string]$Bump = "patch",
    [switch]$All
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function ConvertTo-Hashtable($jsonObject) {
    $table = @{}
    if ($null -eq $jsonObject) { return $table }
    foreach ($p in $jsonObject.PSObject.Properties) {
        $table[$p.Name] = [string]$p.Value
    }
    return $table
}

function Next-Version([string]$v, [string]$part) {
    if ($v -notmatch '^v(\d+)\.(\d+)\.(\d+)$') {
        throw "Unsupported version format: $v (expected vX.Y.Z)"
    }
    $maj=[int]$Matches[1]; $min=[int]$Matches[2]; $pat=[int]$Matches[3]
    switch($part) {
        'major' { $maj++; $min=0; $pat=0 }
        'minor' { $min++; $pat=0 }
        'patch' { $pat++ }
    }
    return "v$maj.$min.$pat"
}

$root = (Get-Location).Path
$path = Join-Path $root $VersionsFile
if (-not (Test-Path $path)) {
    throw "Versions file not found: $VersionsFile"
}

$raw = Get-Content -Raw -LiteralPath $path | ConvertFrom-Json
$map = ConvertTo-Hashtable $raw

if ($All) {
    foreach ($k in @($map.Keys)) {
        $map[$k] = if ($Version) { $Version } else { Next-Version $map[$k] $Bump }
    }
} else {
    if (-not $Module) { throw "-Module is required unless -All is set" }
    if (-not $map.ContainsKey($Module)) { throw "Module not found in versions: $Module" }
    $map[$Module] = if ($Version) { $Version } else { Next-Version $map[$Module] $Bump }
}

$out = [ordered]@{}
foreach ($k in ($map.Keys | Sort-Object)) { $out[$k] = $map[$k] }
$out | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $path
Write-Host "Updated versions: $VersionsFile"
