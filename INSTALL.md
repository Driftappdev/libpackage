# libpackage Multi-Module Installation Guide

Repository: `github.com/platformcore/libpackage`

This repository is configured as **separate Go modules** (not one combined module).
Install only the module you need.

## Example install commands

```bash
go get github.com/platformcore/libpackage/foundation/core/types@latest
go get github.com/platformcore/libpackage/foundation/result@latest
go get github.com/platformcore/libpackage/foundation/core/context@latest
go get github.com/platformcore/libpackage/foundation/core/constants@latest
go get github.com/platformcore/libpackage/foundation/core/errors@latest
go get github.com/platformcore/libpackage/foundation/core/logger@latest
go get github.com/platformcore/libpackage/logmid/logging-middleware@latest
go get github.com/platformcore/libpackage/security/jwt@latest
```


## Other available modules

- `github.com/platformcore/libpackage/security/jwt`
- `github.com/platformcore/libpackage/security/oauth2`
- `github.com/platformcore/libpackage/security/hash`
- `github.com/platformcore/libpackage/foundation/runtime/lifecycle`
- `github.com/platformcore/libpackage/foundation/runtime/shutdown`
- `github.com/platformcore/libpackage/foundation/runtime/health`
- `github.com/platformcore/libpackage/compat/goauth`
- `github.com/platformcore/libpackage/resilience/cache`
- `github.com/platformcore/libpackage/compat/gocircuit`
- `github.com/platformcore/libpackage/compat/goerror`
- `github.com/platformcore/libpackage/compat/gologger`
- `github.com/platformcore/libpackage/compat/gometrics`
- `github.com/platformcore/libpackage/resilience/pagination`
- `github.com/platformcore/libpackage/compat/goratelimit`
- `github.com/platformcore/libpackage/compat/goretry`
- `github.com/platformcore/libpackage/compat/gosanitizer`
- `github.com/platformcore/libpackage/compat/gotimeout`
- `github.com/platformcore/libpackage/compat/gotracing`
- `github.com/platformcore/libpackage/resilience/validate`
- `github.com/platformcore/libpackage/resilience/validator`
- `github.com/platformcore/libpackage/security/encryption`
### Persistence
- `github.com/platformcore/libpackage/persistence/tx`
- `github.com/platformcore/libpackage/persistence/uow`

### Messaging
- `github.com/platformcore/libpackage/messaging/outbox`
- `github.com/platformcore/libpackage/messaging/inbox`
- `github.com/platformcore/libpackage/messaging/dlq`
- `github.com/platformcore/libpackage/messaging/redrive`
- `github.com/platformcore/libpackage/messaging/replay`
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



