# libpackage Multi-Module Installation Guide

Repository: `github.com/driftappdev`

This repository is configured as **separate Go modules** (not one combined module).
Install only the module you need.

## Example install commands

```bash
go get github.com/driftappdev/foundation/core/types@latest
go get github.com/driftappdev/foundation/result@latest
go get github.com/driftappdev/foundation/core/context@latest
go get github.com/driftappdev/foundation/core/constants@latest
go get github.com/driftappdev/foundation/core/errors@latest
go get github.com/driftappdev/foundation/core/logger@latest
go get github.com/driftappdev/logmid/logging-middleware@latest
go get github.com/driftappdev/security/jwt@latest
```


## Other available modules

- `github.com/driftappdev/security/jwt`
- `github.com/driftappdev/security/oauth2`
- `github.com/driftappdev/security/hash`
- `github.com/driftappdev/foundation/runtime/lifecycle`
- `github.com/driftappdev/foundation/runtime/shutdown`
- `github.com/driftappdev/foundation/runtime/health`
- `github.com/driftappdev/compat/goauth`
- `github.com/driftappdev/resilience/cache`
- `github.com/driftappdev/compat/gocircuit`
- `github.com/driftappdev/compat/goerror`
- `github.com/driftappdev/compat/gologger`
- `github.com/driftappdev/compat/gometrics`
- `github.com/driftappdev/resilience/pagination`
- `github.com/driftappdev/compat/goratelimit`
- `github.com/driftappdev/compat/goretry`
- `github.com/driftappdev/compat/gosanitizer`
- `github.com/driftappdev/compat/gotimeout`
- `github.com/driftappdev/compat/gotracing`
- `github.com/driftappdev/resilience/validate`
- `github.com/driftappdev/resilience/validator`
- `github.com/driftappdev/security/encryption`
### Persistence
- `github.com/driftappdev/persistence/tx`
- `github.com/driftappdev/persistence/uow`

### Messaging
- `github.com/driftappdev/messaging/outbox`
- `github.com/driftappdev/messaging/inbox`
- `github.com/driftappdev/messaging/dlq`
- `github.com/driftappdev/messaging/redrive`
- `github.com/driftappdev/messaging/replay`
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



