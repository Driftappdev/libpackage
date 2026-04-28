param(
    [ValidateSet("major","minor","patch")][string]$Bump = "patch",
    [string]$Version,
    [ValidateSet("all","standard","filemods")][string]$Scope = "all",
    [string]$DefaultVersion = "v0.1.0",
    [string]$Branch = "main",
    [string]$CommitMessage = "chore(release): bump module versions and tags",
    [switch]$NoCommit,
    [switch]$NoPush,
    [switch]$NoTagCreate,
    [switch]$NoTagPush,
    [switch]$DryRun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $repoRoot

$standardVersionsFile = "tools/module-versions.json"
$fileVersionsFile = "tools/file-module-versions.json"

function Require-Cmd([string]$name) {
    if (-not (Get-Command $name -ErrorAction SilentlyContinue)) {
        throw "Required command not found: $name"
    }
}

function ConvertTo-Hashtable($jsonObject) {
    $table = @{}
    if ($null -eq $jsonObject) { return $table }
    foreach ($p in $jsonObject.PSObject.Properties) {
        $table[$p.Name] = [string]$p.Value
    }
    return $table
}

function Read-Versions([string]$Path) {
    if (-not (Test-Path $Path)) { return @{} }
    $raw = Get-Content -Raw -LiteralPath $Path | ConvertFrom-Json
    return ConvertTo-Hashtable $raw
}

function Write-Versions([string]$Path, [hashtable]$Map) {
    $ordered = [ordered]@{}
    foreach ($k in ($Map.Keys | Sort-Object)) {
        $ordered[$k] = [string]$Map[$k]
    }
    $ordered | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $Path
}

function Next-Version([string]$v, [string]$part) {
    if ($v -notmatch '^v(\d+)\.(\d+)\.(\d+)$') {
        throw "Unsupported version format: $v (expected vX.Y.Z)"
    }
    $maj=[int]$Matches[1]; $min=[int]$Matches[2]; $pat=[int]$Matches[3]
    switch($part) {
        "major" { $maj++; $min=0; $pat=0 }
        "minor" { $min++; $pat=0 }
        "patch" { $pat++ }
    }
    return "v$maj.$min.$pat"
}

function Get-RelativePath([string]$FromPath, [string]$ToPath) {
    $fromUri = New-Object System.Uri(($FromPath.TrimEnd('\') + '\'))
    $toUri = New-Object System.Uri(($ToPath.TrimEnd('\') + '\'))
    $relativeUri = $fromUri.MakeRelativeUri($toUri).ToString()
    $relativePath = [System.Uri]::UnescapeDataString($relativeUri).Replace('/', '/')
    if ([string]::IsNullOrWhiteSpace($relativePath)) { return "." }
    return $relativePath.TrimEnd('/')
}

function Get-Modules([string]$RepoRoot) {
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
            }
        }

    $dup = $mods | Group-Object ModulePath | Where-Object { $_.Count -gt 1 }
    if ($dup) {
        $names = ($dup | ForEach-Object { $_.Name }) -join ", "
        throw "Duplicate module path(s): $names"
    }
    return @($mods | Sort-Object ModulePath)
}

function Is-FileMod([string]$ModulePath) {
    return $ModulePath -like "github.com/driftappdev/filemods/*"
}

function Run([string]$cmd) {
    Write-Host "> $cmd"
    if (-not $DryRun) {
        Invoke-Expression $cmd
        if ($LASTEXITCODE -ne 0) {
            throw "Command failed: $cmd"
        }
    }
}

Require-Cmd git

$allModules = Get-Modules -RepoRoot $repoRoot
$selected = @()
switch ($Scope) {
    "all" {
        $selected = $allModules
    }
    "standard" {
        $selected = @($allModules | Where-Object { -not (Is-FileMod $_.ModulePath) })
    }
    "filemods" {
        $selected = @($allModules | Where-Object { Is-FileMod $_.ModulePath })
    }
}

if ($selected.Count -eq 0) {
    throw "No modules found for scope: $Scope"
}

$standardVersions = Read-Versions $standardVersionsFile
$fileVersions = Read-Versions $fileVersionsFile

foreach ($m in $allModules) {
    if (Is-FileMod $m.ModulePath) {
        if (-not $fileVersions.ContainsKey($m.ModulePath)) {
            $fileVersions[$m.ModulePath] = $DefaultVersion
        }
    } else {
        if (-not $standardVersions.ContainsKey($m.ModulePath)) {
            $standardVersions[$m.ModulePath] = $DefaultVersion
        }
    }
}

$currentSetStandard = @{}
$currentSetFile = @{}
foreach ($m in $allModules) {
    if (Is-FileMod $m.ModulePath) { $currentSetFile[$m.ModulePath] = $true }
    else { $currentSetStandard[$m.ModulePath] = $true }
}
foreach ($k in @($standardVersions.Keys)) {
    if (-not $currentSetStandard.ContainsKey($k)) { $standardVersions.Remove($k) }
}
foreach ($k in @($fileVersions.Keys)) {
    if (-not $currentSetFile.ContainsKey($k)) { $fileVersions.Remove($k) }
}

$changed = 0
foreach ($m in $selected) {
    if (Is-FileMod $m.ModulePath) {
        $cur = [string]$fileVersions[$m.ModulePath]
        $next = if ($Version) { $Version } else { Next-Version $cur $Bump }
        $fileVersions[$m.ModulePath] = $next
        $changed++
    } else {
        $cur = [string]$standardVersions[$m.ModulePath]
        $next = if ($Version) { $Version } else { Next-Version $cur $Bump }
        $standardVersions[$m.ModulePath] = $next
        $changed++
    }
}

if ($Version -and $Version -notmatch '^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') {
    throw "Invalid -Version format: $Version"
}

if (-not $DryRun) {
    Write-Versions -Path $standardVersionsFile -Map $standardVersions
    Write-Versions -Path $fileVersionsFile -Map $fileVersions
} else {
    Write-Host "DryRun: skip writing versions files"
}

$tagCount = 0
if (-not $NoTagCreate) {
    foreach ($m in $selected) {
        $v = if (Is-FileMod $m.ModulePath) { [string]$fileVersions[$m.ModulePath] } else { [string]$standardVersions[$m.ModulePath] }
        if ($v -notmatch '^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') {
            throw "Invalid semver for $($m.ModulePath): $v"
        }

        $tag = if ($m.RelDir -eq ".") { $v } else { "$($m.RelDir)/$v" }

        $existsLocal = $false
        if (-not $DryRun) {
            git rev-parse -q --verify "refs/tags/$tag" *> $null
            $existsLocal = ($LASTEXITCODE -eq 0)
        }

        if (-not $existsLocal) {
            Run "git tag $tag"
            $tagCount++
        } else {
            Write-Host "Tag exists (skip): $tag"
        }
    }
} else {
    Write-Host "Skip tag creation (-NoTagCreate)."
}

if (-not $NoCommit) {
    $currentBranch = (git rev-parse --abbrev-ref HEAD).Trim()
    if ($currentBranch -ne $Branch) {
        Run "git checkout $Branch"
    }

    Run "git add $standardVersionsFile $fileVersionsFile"
    $hasChanges = $true
    if (-not $DryRun) {
        git diff --cached --quiet
        $hasChanges = ($LASTEXITCODE -ne 0)
    }
    if ($hasChanges) {
        Run "git commit -m '$CommitMessage'"
    } else {
        Write-Host "No version-file changes to commit."
    }
}

if (-not $NoPush) {
    Run "git push origin $Branch"
}

if ((-not $NoTagPush) -and (-not $NoTagCreate)) {
    Run "git push --tags"
}

Write-Host ""
Write-Host "Release completed."
Write-Host "Scope: $Scope"
Write-Host "Modules touched: $changed"
Write-Host "New tags created: $tagCount"

