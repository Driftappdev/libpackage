param(
    [Parameter(Mandatory = $true)][string]$GitHubOwner,
    [string]$RepositoryName = "libpackage",
    [ValidateSet("public","private")][string]$Visibility = "public",
    [string]$Branch = "main",
    [string]$CommitMessage = "chore: publish modules",
    [string]$VersionsFile = "tools/module-versions.json",
    [string[]]$OnlyPath,
    [string[]]$OnlyModule,
    [switch]$SkipCommit,
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

Require-Cmd git
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
            $modulePath = $first.Substring(7).Trim()
            $dir = Split-Path -Parent $_.FullName
            $relDir = Get-RelativePath -FromPath $RepoRoot -ToPath $dir
            if ([string]::IsNullOrWhiteSpace($relDir) -or $relDir -eq ".") { $relDir = "." }

            $mods += [PSCustomObject]@{
                ModulePath = $modulePath
                RelDir     = $relDir
            }
        }
    return @($mods | Sort-Object ModulePath)
}

function Normalize-PathPrefix([string]$pathPrefix) {
    if ([string]::IsNullOrWhiteSpace($pathPrefix)) { return $null }
    $normalized = $pathPrefix.Replace('\', '/').Trim()
    return $normalized.Trim('/')
}

function Match-PathPrefix([string]$relDir, [string]$prefix) {
    if ([string]::IsNullOrWhiteSpace($prefix)) { return $false }
    if ($relDir -eq $prefix) { return $true }
    return $relDir.StartsWith("$prefix/")
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

if (-not (Test-Path ".git")) {
    Run "git init -b $Branch"
}

# Ensure branch exists/current
$currentBranch = (git rev-parse --abbrev-ref HEAD).Trim()
if ($currentBranch -ne $Branch) {
    Run "git checkout -B $Branch"
}

if (-not $SkipCommit) {
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
} else {
    Write-Host "Skip commit (-SkipCommit)."
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
        Require-Cmd gh
        if (-not $DryRun) {
            gh auth status | Out-Null
        }
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
    throw "Versions file not found: $VersionsFile`nRun: .\tools\generate-modules.ps1"
}

$versions = ConvertTo-Hashtable (Get-Content -Raw -LiteralPath $versionsPath | ConvertFrom-Json)
$modules = Get-ModuleItems -RepoRoot $repoRoot

if ($OnlyPath -and $OnlyPath.Count -gt 0) {
    $normalizedPaths = @(
        $OnlyPath |
            ForEach-Object { ($_ -split '[,;]') } |
            ForEach-Object { Normalize-PathPrefix $_ } |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) } |
            Sort-Object -Unique
    )
    $modules = @(
        $modules | Where-Object {
            $m = $_
            $matched = $false
            foreach ($p in $normalizedPaths) {
                if (Match-PathPrefix -relDir $m.RelDir -prefix $p) {
                    $matched = $true
                    break
                }
            }
            $matched
        }
    )
}

if ($OnlyModule -and $OnlyModule.Count -gt 0) {
    $moduleSet = @{}
    foreach ($moduleName in $OnlyModule) {
        if (-not [string]::IsNullOrWhiteSpace($moduleName)) {
            $moduleSet[$moduleName.Trim()] = $true
        }
    }
    $modules = @($modules | Where-Object { $moduleSet.ContainsKey($_.ModulePath) })
}

if ($modules.Count -eq 0) {
    throw "No modules selected for publish. Check -OnlyPath or -OnlyModule filters."
}

foreach ($m in $modules) {
    if (-not $versions.Contains($m.ModulePath)) {
        throw "Missing version for module in $VersionsFile : $($m.ModulePath)"
    }

    $version = [string]$versions[$m.ModulePath]
    if ($version -notmatch '^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') {
        throw "Invalid semver for $($m.ModulePath): $version"
    }

    $tag = if ($m.RelDir -eq ".") { $version } else { "$($m.RelDir)/$version" }

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
Write-Host "Modules tagged: $($modules.Count)"
