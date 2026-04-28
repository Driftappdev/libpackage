param(
  [string]$MatrixFile = "PACKAGE_RELEASE_MATRIX.md",
  [string]$Remote = "origin",
  [string]$Branch = "main",
  [switch]$SkipBranchPush
)

$ErrorActionPreference = "Stop"
if (!(Test-Path $MatrixFile)) {
  throw "Matrix file not found: $MatrixFile"
}

$rows = @()
Get-Content $MatrixFile | ForEach-Object {
  if ($_ -match '^\|\s*([^|]+?)\s*\|\s*(v\d+\.\d+\.\d+)\s*\|\s*$' -and $_ -notmatch 'Module Path') {
    $rows += [pscustomobject]@{
      path = $matches[1].Trim()
      version = $matches[2].Trim()
    }
  }
}

if ($rows.Count -eq 0) {
  throw "No release rows found in $MatrixFile"
}

git fetch --tags $Remote
if (-not $SkipBranchPush) {
  git push $Remote $Branch
}

foreach ($r in $rows) {
  $tag = "$($r.path)/$($r.version)"
  if (-not (git tag --list $tag)) {
    git tag -a $tag -m "release $tag"
  }
  git push $Remote $tag
}

Write-Host "Done releasing all tags from matrix"
