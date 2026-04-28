# Version Update Commands

Run all commands from `libpackage` root.

## 1) Add new module or sync module list

When adding/removing `go.mod`, regenerate module metadata first:

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\generate-modules.ps1
```

This updates:
- `tools/module-versions.json`
- `INSTALL_MODULES.md`
- `MODULE_CATALOG.md`

## 2) Update version for one standard module

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\bump-module-version.ps1 `
  -Module github.com/platformcore/libpackage/persistence/uow `
  -Version v0.1.1
```

Or auto-bump patch:

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\bump-module-version.ps1 `
  -Module github.com/platformcore/libpackage/messaging/outbox `
  -Bump patch
```

## 3) Update version for all standard modules at once

Set explicit version:

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\bump-module-version.ps1 -All -Version v0.1.1
```

Or bump all with semver:

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\bump-module-version.ps1 -All -Bump patch
```

## 4) Update file-modules versions (optional)

One module:

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\bump-file-module-version.ps1 `
  -Module github.com/platformcore/libpackage/filemods/core/result/result `
  -Version v0.1.1
```

All file-modules:

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\bump-file-module-version.ps1 -All -Bump patch
```

## 5) Release standard + file-modules together in one command

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\release-modules.ps1 -Scope all -Version v0.1.1
```

## 6) Publish only `persistence` and `messaging` tags to GitHub

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\publish-github.ps1 `
  -GitHubOwner driftappdev `
  -RepositoryName libpackage `
  -OnlyPath persistence,messaging `
  -SkipCommit `
  -SkipRepoCreate
```

Preview first (no real changes):

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\publish-github.ps1 `
  -GitHubOwner driftappdev `
  -RepositoryName libpackage `
  -OnlyPath persistence,messaging `
  -SkipCommit `
  -SkipRepoCreate `
  -DryRun
```

