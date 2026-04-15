# libpackage Multi-Module Installation Guide

Repository: `github.com/driftappdev/libpackage`

This repository is configured as **separate Go modules** (not one combined module).
Install only the module you need.

## Example install commands

```bash
go get github.com/driftappdev/libpackage/core/types@latest
go get github.com/driftappdev/libpackage/result@latest
go get github.com/driftappdev/libpackage/core/context@latest
go get github.com/driftappdev/libpackage/core/constants@latest
go get github.com/driftappdev/libpackage/core/errors@latest
go get github.com/driftappdev/libpackage/core/logger@latest
go get github.com/driftappdev/libpackage/logmid/logging-middleware@latest
go get github.com/driftappdev/libpackage/security/jwt@latest
```

## Other available modules

- `github.com/driftappdev/libpackage/security/jwt`
- `github.com/driftappdev/libpackage/security/oauth2`
- `github.com/driftappdev/libpackage/security/hash`
- `github.com/driftappdev/libpackage/runtime/lifecycle`
- `github.com/driftappdev/libpackage/runtime/shutdown`
- `github.com/driftappdev/libpackage/runtime/health`
- `github.com/driftappdev/libpackage/goauth`
- `github.com/driftappdev/libpackage/resilience/cache`
- `github.com/driftappdev/libpackage/gocircuit`
- `github.com/driftappdev/libpackage/goerror`
- `github.com/driftappdev/libpackage/gologger`
- `github.com/driftappdev/libpackage/gometrics`
- `github.com/driftappdev/libpackage/resilience/pagination`
- `github.com/driftappdev/libpackage/goratelimit`
- `github.com/driftappdev/libpackage/goretry`
- `github.com/driftappdev/libpackage/gosanitizer`
- `github.com/driftappdev/libpackage/gotimeout`
- `github.com/driftappdev/libpackage/gotracing`
- `github.com/driftappdev/libpackage/resilience/validate`
- `github.com/driftappdev/libpackage/resilience/validator`
- `github.com/driftappdev/libpackage/security/encryption`

## Local development in this repo

- This repo now uses `go.work` to develop multiple modules together.
- Each module has its own `go.mod`.

## Tagging when publishing

For submodules, use tags by module path prefix, for example:

```bash
git tag types/v1.0.0
git tag result/v1.0.0
git tag security/jwt/v1.0.0
git tag logmid/logging-middleware/v1.0.0
```

Then push tags:

```bash
git push origin --tags
```
