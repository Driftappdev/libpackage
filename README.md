# libpackage

Multi-module Go packages published under:

- `github.com/platformcore/libpackage/...`

## One-time GitHub setup

```powershell
$env:GITHUB_TOKEN = "<your_token_with_repo_scope>"
.\scripts\create-github-repo.ps1 -Owner driftapp -Repo libpackage -DefaultBranch main
```

## One-command release per module

```powershell
.\scripts\release.ps1 -Module eventbus/retry -Version v0.1.0
```

This command will:

1. commit current changes
2. create annotated tag `eventbus/retry/v0.1.0`
3. push branch + tag to GitHub

Consumers install with version:

```bash
go get github.com/platformcore/libpackage/platform/eventbus/retry@v0.1.0
```


