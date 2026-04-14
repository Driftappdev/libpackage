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

## 4) Install from GitHub

Use commands in `INSTALL_MODULES.md`, for example:

```bash
go get github.com/driftappdev/libpackage/core/result@latest
```
