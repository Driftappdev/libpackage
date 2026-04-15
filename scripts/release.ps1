param(
  [Parameter(Mandatory=$true)]
  [string]$Module,

  [Parameter(Mandatory=$true)]
  [ValidatePattern('^v\d+\.\d+\.\d+([-.][0-9A-Za-z.-]+)?$')]
  [string]$Version,

  [string]$Remote = 'origin',
  [string]$Branch = 'main',
  [string]$Message
)

$ErrorActionPreference = 'Stop'

function Run([string]$cmd) {
  Write-Host "> $cmd"
  Invoke-Expression $cmd
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $repoRoot

if (-not (Test-Path '.git')) {
  throw 'This folder is not a git repository. Run scripts/create-github-repo.ps1 first.'
}

$modulePath = ($Module -replace '\\','/').Trim('/').Trim()
if ($modulePath -eq '.') { $modulePath = '' }

$moduleDir = if ($modulePath) { Join-Path $repoRoot $modulePath } else { $repoRoot }
$goModPath = Join-Path $moduleDir 'go.mod'
if (-not (Test-Path $goModPath)) {
  throw "go.mod not found at: $goModPath"
}

$tag = if ($modulePath) { "$modulePath/$Version" } else { $Version }
if (-not $Message) {
  $target = if ($modulePath) { $modulePath } else { 'root' }
  $Message = "release($target): $Version"
}

$branchExists = git rev-parse --verify $Branch 2>$null
if (-not $branchExists) {
  Run "git checkout -B $Branch"
} else {
  Run "git checkout $Branch"
}

Run 'git add -A'
$hasChanges = (git status --porcelain)
if ($hasChanges) {
  Run "git commit -m '$Message'"
} else {
  Write-Host 'No file changes to commit.'
}

$tagExists = git tag --list $tag
if ($tagExists) {
  throw "Tag already exists: $tag"
}

Run "git tag -a $tag -m 'Release $tag'"
Run "git push $Remote $Branch"
Run "git push $Remote $tag"

Write-Host "Released module '$modulePath' as tag '$tag'"
