param(
  [Parameter(Mandatory=$true)][string]$ModulePath,
  [Parameter(Mandatory=$true)][string]$Version,
  [string]$Remote = "origin",
  [string]$Branch = "main",
  [switch]$PushTagOnly
)

$ErrorActionPreference = "Stop"
if ($Version -notmatch '^v\d+\.\d+\.\d+$') {
  throw "Version must be semantic, e.g. v1.2.3"
}

$tag = "$ModulePath/$Version"
Write-Host "[release-one] module=$ModulePath version=$Version tag=$tag"

git fetch --tags $Remote
if (-not (git tag --list $tag)) {
  git tag -a $tag -m "release $tag"
} else {
  Write-Host "Tag already exists: $tag"
}

if (-not $PushTagOnly) {
  git push $Remote $Branch
}
git push $Remote $tag

Write-Host "Done: $tag"
