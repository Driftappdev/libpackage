# GitHub Publish Guide

This repository is a multi-module Go monorepo.
Each module can be versioned independently with tags.

## 1) Refresh module metadata

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\generate-modules.ps1
```

This regenerates:
- `INSTALL_MODULES.md` (all `go get` commands)
- `MODULE_CATALOG.md` (module -> folder map)
- `tools/module-versions.json` (per-module versions)

## 2) Edit per-module versions

Update `tools/module-versions.json`.

Rules:
- Root module tag: `vX.Y.Z`
- Submodule tag: `<subdir>/vX.Y.Z`
  example: `core/result/v1.4.0`

The script builds tags automatically from the module folder and version.

## 3) Create/push repo and tags

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\publish-github.ps1 `
  -GitHubOwner driftappdev `
  -RepositoryName libpackage `
  -Visibility public
```

Optional flags:
- `-DryRun` : print commands only
- `-SkipRepoCreate` : use existing repo/remote only
- `-SkipTagPush` : push branch but do not push tags
- `-SkipCommit` : do not run `git add/commit` in script
- `-OnlyPath` : publish tags for selected folders only (example: `persistence,messaging`)
- `-OnlyModule` : publish tags for selected module path(s) only

## 4) Install from GitHub

Use commands in `INSTALL_MODULES.md`, for example:

```bash
go get github.com/platformcore/libpackage/foundation/core/result@latest
```

## One-command release (bump + tag + push)

Use this when you want one command to bump versions, create tags, and push.

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\release-modules.ps1 -Scope all -Bump patch
```

Common examples:

```powershell
# bump all standard modules only
powershell -ExecutionPolicy Bypass -File .\tools\release-modules.ps1 -Scope standard -Bump minor

# bump only file-level modules
powershell -ExecutionPolicy Bypass -File .\tools\release-modules.ps1 -Scope filemods -Bump patch

# set explicit version for selected scope
powershell -ExecutionPolicy Bypass -File .\tools\release-modules.ps1 -Scope all -Version v1.0.0

# safe preview (no write/commit/push)
powershell -ExecutionPolicy Bypass -File .\tools\release-modules.ps1 -Scope all -Bump patch -DryRun
```

## Publish only selected folders

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\publish-github.ps1 `
  -GitHubOwner driftappdev `
  -RepositoryName libpackage `
  -OnlyPath persistence,messaging `
  -SkipRepoCreate
```


