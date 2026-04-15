# Run Commands

Run all commands from `lib_package` root.

## 1) Update version only (no commit, no push, no tags)

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\commands\01-update-version-only.ps1 -Scope all -Bump patch
```

Examples:
```powershell
powershell -ExecutionPolicy Bypass -File .\tools\commands\01-update-version-only.ps1 -Scope standard -Bump minor
powershell -ExecutionPolicy Bypass -File .\tools\commands\01-update-version-only.ps1 -Scope filemods -Version v1.0.0
```

## 2) Update all generated files

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\commands\02-update-all-generated.ps1
```

## 3) Update code (git add + commit)

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\commands\03-update-code-commit.ps1 -Message "chore: update libs"
```

## 4) Upload to GitHub (push)

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\commands\04-upload-github.ps1 -Branch main
```

Push with tags:
```powershell
powershell -ExecutionPolicy Bypass -File .\tools\commands\04-upload-github.ps1 -Branch main -WithTags
```

## 5) Full release (bump + tag + commit + push)

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\commands\05-release-all.ps1 -Scope all -Bump patch
```

Examples:
```powershell
powershell -ExecutionPolicy Bypass -File .\tools\commands\05-release-all.ps1 -Scope standard -Bump minor
powershell -ExecutionPolicy Bypass -File .\tools\commands\05-release-all.ps1 -Scope filemods -Version v1.2.0
```

