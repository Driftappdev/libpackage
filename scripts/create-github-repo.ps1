param(
  [string]$Owner = 'driftapp',
  [string]$Repo = 'libpackage',
  [string]$DefaultBranch = 'main',
  [string]$Remote = 'origin',
  [string]$Visibility = 'public'
)

$ErrorActionPreference = 'Stop'

function Ensure-GitRepo {
  if (-not (Test-Path '.git')) {
    git init
  }
}

function Run([string]$cmd) {
  Write-Host "> $cmd"
  Invoke-Expression $cmd
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $repoRoot

Ensure-GitRepo

if (-not (Test-Path '.gitignore')) {
@"
# Go
bin/
*.test
*.out

# OS
.DS_Store
Thumbs.db

# IDE
.vscode/
.idea/
"@ | Set-Content -Path .gitignore
}

$token = $env:GITHUB_TOKEN
if (-not $token) {
  throw 'Set GITHUB_TOKEN before running this script. It needs repo create permission.'
}

$remoteUrl = "https://github.com/$Owner/$Repo.git"
$apiUrl = "https://api.github.com/repos/$Owner/$Repo"
$headers = @{
  Authorization = "Bearer $token"
  Accept        = 'application/vnd.github+json'
  'User-Agent'  = 'libpackage-release-script'
}

$repoExists = $true
try {
  Invoke-RestMethod -Method Get -Uri $apiUrl -Headers $headers | Out-Null
} catch {
  $repoExists = $false
}

if (-not $repoExists) {
  $ownerType = 'Organization'
  try {
    $ownerInfo = Invoke-RestMethod -Method Get -Uri "https://api.github.com/users/$Owner" -Headers $headers
    if ($ownerInfo.type) {
      $ownerType = [string]$ownerInfo.type
    }
  } catch {
    throw "Cannot resolve GitHub owner '$Owner'."
  }

  if ($ownerType -eq 'User') {
    $createUrl = 'https://api.github.com/user/repos'
  } else {
    $createUrl = "https://api.github.com/orgs/$Owner/repos"
  }

  $body = @{
    name            = $Repo
    private         = ($Visibility -eq 'private')
    auto_init       = $false
    has_issues      = $true
    has_projects    = $false
    has_wiki        = $false
    delete_branch_on_merge = $true
  } | ConvertTo-Json

  Write-Host "> Creating GitHub repository $Owner/$Repo"
  Invoke-RestMethod -Method Post -Uri $createUrl -Headers $headers -Body $body -ContentType 'application/json' | Out-Null
}

Run "git checkout -B $DefaultBranch"

$existingRemote = git remote
if ($existingRemote -notcontains $Remote) {
  Run "git remote add $Remote $remoteUrl"
} else {
  Run "git remote set-url $Remote $remoteUrl"
}

Run 'git add -A'
$hasChanges = (git status --porcelain)
if ($hasChanges) {
  Run "git commit -m 'chore: initial publish setup'"
}

Run "git push -u $Remote $DefaultBranch"
Write-Host "Repository is ready: $remoteUrl"
