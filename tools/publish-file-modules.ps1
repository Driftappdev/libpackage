param(
    [Parameter(Mandatory = $true)][string]$GitHubOwner,
    [string]$RepositoryName = "libpackage",
    [ValidateSet("public","private")][string]$Visibility = "public",
    [string]$Branch = "main",
    [string]$CommitMessage = "chore: publish file_modules",
    [string]$VersionsFile = "tools/file-module-versions.json",
    [string]$ModulesRoot = "file_modules",
    [switch]$SkipRepoCreate,
    [switch]$SkipTagPush,
    [switch]$DryRun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $repoRoot

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

function Get-RelativePath([string]$FromPath, [string]$ToPath) {
    $fromUri = New-Object System.Uri(($FromPath.TrimEnd('\\') + '\\'))
    $toUri = New-Object System.Uri(($ToPath.TrimEnd('\\') + '\\'))
    $relativeUri = $fromUri.MakeRelativeUri($toUri).ToString()
    $relativePath = [System.Uri]::UnescapeDataString($relativeUri).Replace('/', '/')
    if ([string]::IsNullOrWhiteSpace($relativePath)) { return "." }
    return $relativePath.TrimEnd('/')
}

function Get-ModuleItems {
    param([string]$RepoRoot, [string]$OnlyUnder)

    $rootUnder = (Resolve-Path (Join-Path $RepoRoot $OnlyUnder)).Path
    $mods = @()

    Get-ChildItem -Path $rootUnder -Recurse -File -Filter go.mod |
        ForEach-Object {
            $first = (Get-Content -LiteralPath $_.FullName -TotalCount 1).Trim()
            if (-not $first.StartsWith("module ")) {
                throw "Invalid go.mod: $($_.FullName)"
            }
            $modulePath = $first.Substring(7).Trim()
            $dir = Split-Path -Parent $_.FullName
            $relDir = Get-RelativePath -FromPath $RepoRoot -ToPath $dir

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
Require-Cmd gh

if (-not $DryRun) {
    gh auth status | Out-Null
}

if (-not (Test-Path ".git")) {
    Run "git init -b $Branch"
}

$currentBranch = (git rev-parse --abbrev-ref HEAD).Trim()
if ($currentBranch -ne $Branch) {
    Run "git checkout -B $Branch"
}

Run "git add -A"
$hasChanges = $true
if (-not $DryRun) {
    git diff --cached --quiet
    $hasChanges = ($LASTEXITCODE -ne 0)
}
if ($hasChanges) {
    Run "git commit -m '$CommitMessage'"
} else {
    Write-Host "No staged changes to commit."
}

$repoFull = "$GitHubOwner/$RepositoryName"
$remoteUrl = "https://github.com/$repoFull.git"

$originExists = $false
if (-not $DryRun) {
    git remote get-url origin *> $null
    $originExists = ($LASTEXITCODE -eq 0)
}

if (-not $originExists) {
    if (-not $SkipRepoCreate) {
        Run "gh repo create $repoFull --$Visibility --source . --remote origin --push"
    } else {
        Run "git remote add origin $remoteUrl"
        Run "git push -u origin $Branch"
    }
} else {
    Run "git remote set-url origin $remoteUrl"
    Run "git push -u origin $Branch"
}

$versionsPath = Join-Path $repoRoot $VersionsFile
if (-not (Test-Path $versionsPath)) {
    throw "Versions file not found: $VersionsFile"
}

$versions = ConvertTo-Hashtable (Get-Content -Raw -LiteralPath $versionsPath | ConvertFrom-Json)
$modules = Get-ModuleItems -RepoRoot $repoRoot -OnlyUnder $ModulesRoot

foreach ($m in $modules) {
    if (-not $versions.Contains($m.ModulePath)) {
        throw "Missing version in $VersionsFile : $($m.ModulePath)"
    }

    $version = [string]$versions[$m.ModulePath]
    if ($version -notmatch '^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') {
        throw "Invalid semver for $($m.ModulePath): $version"
    }

    $tag = "$($m.RelDir)/$version"

    $existsLocal = $false
    if (-not $DryRun) {
        git rev-parse -q --verify "refs/tags/$tag" *> $null
        $existsLocal = ($LASTEXITCODE -eq 0)
    }

    if (-not $existsLocal) {
        Run "git tag $tag"
    } else {
        Write-Host "Tag exists (skip): $tag"
    }
}

if (-not $SkipTagPush) {
    Run "git push --tags"
}

Write-Host "Publish flow complete for $repoFull"
Write-Host "File-modules tagged: $($modules.Count)"
