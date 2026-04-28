# Module Structure (Mainstream Style)

This repository follows a domain-first Go module layout:
- Main module = domain root (can be installed alone)
- Submodule = optional focused package under that domain
- Submodules are not required unless imported by your app or required by the main module

## Main Modules (With Submodules)

- `github.com/platformcore/libpackage/foundation/client`
- `github.com/platformcore/libpackage/foundation/contracts`
- `github.com/platformcore/libpackage/foundation/core`
- `github.com/platformcore/libpackage/orchestration/di`
- `github.com/platformcore/libpackage/platform/eventbus`
- `github.com/platformcore/libpackage/platform/featureflag`
- `github.com/platformcore/libpackage/infra`
- `github.com/platformcore/libpackage/observability`
- `github.com/platformcore/libpackage/platform`
- `github.com/platformcore/libpackage/plugins`
- `github.com/platformcore/libpackage/resilience`
- `github.com/platformcore/libpackage/foundation/runtime`
- `github.com/platformcore/libpackage/observability/telemetry`
- `github.com/platformcore/libpackage/testing`
- `github.com/platformcore/libpackage/foundation/validator`

## Standalone Main Modules (No Submodules)

- `github.com/platformcore/libpackage/foundation/config`
- `github.com/platformcore/libpackage/docs`
- `github.com/platformcore/libpackage/compat/goauth`
- `github.com/platformcore/libpackage/compat/gocircuit`
- `github.com/platformcore/libpackage/compat/goerror`
- `github.com/platformcore/libpackage/compat/gologger`
- `github.com/platformcore/libpackage/compat/gometrics`
- `github.com/platformcore/libpackage/compat/goratelimit`
- `github.com/platformcore/libpackage/compat/goretry`
- `github.com/platformcore/libpackage/compat/gosanitizer`
- `github.com/platformcore/libpackage/compat/gotimeout`
- `github.com/platformcore/libpackage/compat/gotracing`
- `github.com/platformcore/libpackage/logmid/logging-middleware`
- `github.com/platformcore/libpackage/ratelimit`
- `github.com/platformcore/libpackage/foundation/result`
- `github.com/platformcore/libpackage/security/encryption`
- `github.com/platformcore/libpackage/security/hash`
- `github.com/platformcore/libpackage/security/jwt`
- `github.com/platformcore/libpackage/security/oauth2`
- `github.com/platformcore/libpackage/security/secrets`

## Submodules

- `github.com/platformcore/libpackage/foundation/client/grpc`
- `github.com/platformcore/libpackage/foundation/client/http`
- `github.com/platformcore/libpackage/foundation/client/nats`
- `github.com/platformcore/libpackage/foundation/contracts/errors`
- `github.com/platformcore/libpackage/foundation/contracts/pagination`
- `github.com/platformcore/libpackage/foundation/contracts/response`
- `github.com/platformcore/libpackage/foundation/contracts/versioning`
- `github.com/platformcore/libpackage/foundation/core/constants`
- `github.com/platformcore/libpackage/foundation/core/context`
- `github.com/platformcore/libpackage/foundation/core/errors`
- `github.com/platformcore/libpackage/foundation/core/logger`
- `github.com/platformcore/libpackage/foundation/core/result`
- `github.com/platformcore/libpackage/foundation/core/types`
- `github.com/platformcore/libpackage/foundation/core/utils`
- `github.com/platformcore/libpackage/orchestration/di/container`
- `github.com/platformcore/libpackage/orchestration/di/module`
- `github.com/platformcore/libpackage/orchestration/di/provider`
- `github.com/platformcore/libpackage/orchestration/di/registry`
- `github.com/platformcore/libpackage/orchestration/di/scope`
- `github.com/platformcore/libpackage/platform/eventbus/deadletter`
- `github.com/platformcore/libpackage/platform/eventbus/envelope`
- `github.com/platformcore/libpackage/platform/eventbus/headers`
- `github.com/platformcore/libpackage/platform/eventbus/idempotency`
- `github.com/platformcore/libpackage/platform/eventbus/publisher`
- `github.com/platformcore/libpackage/platform/eventbus/registry`
- `github.com/platformcore/libpackage/platform/eventbus/retry`
- `github.com/platformcore/libpackage/platform/eventbus/serializer`
- `github.com/platformcore/libpackage/platform/eventbus/subscriber`
- `github.com/platformcore/libpackage/platform/featureflag/cache`
- `github.com/platformcore/libpackage/platform/featureflag/client`
- `github.com/platformcore/libpackage/platform/featureflag/evaluator`
- `github.com/platformcore/libpackage/platform/featureflag/provider`
- `github.com/platformcore/libpackage/platform/featureflag/types`
- `github.com/platformcore/libpackage/infra/backoff`
- `github.com/platformcore/libpackage/infra/bulkhead`
- `github.com/platformcore/libpackage/infra/cache`
- `github.com/platformcore/libpackage/infra/circuit`
- `github.com/platformcore/libpackage/infra/clock`
- `github.com/platformcore/libpackage/infra/retry`
- `github.com/platformcore/libpackage/observability/correlation`
- `github.com/platformcore/libpackage/observability/healthcheck`
- `github.com/platformcore/libpackage/observability/logging`
- `github.com/platformcore/libpackage/observability/profiler`
- `github.com/platformcore/libpackage/observability/span`
- `github.com/platformcore/libpackage/observability/trace`
- `github.com/platformcore/libpackage/observability/tracing`
- `github.com/platformcore/libpackage/platform/client`
- `github.com/platformcore/libpackage/platform/container`
- `github.com/platformcore/libpackage/platform/evaluator`
- `github.com/platformcore/libpackage/platform/hooks`
- `github.com/platformcore/libpackage/platform/loader`
- `github.com/platformcore/libpackage/platform/provider`
- `github.com/platformcore/libpackage/platform/registry`
- `github.com/platformcore/libpackage/platform/versioning`
- `github.com/platformcore/libpackage/plugins/hooks`
- `github.com/platformcore/libpackage/plugins/loader`
- `github.com/platformcore/libpackage/plugins/manifest`
- `github.com/platformcore/libpackage/plugins/registry`
- `github.com/platformcore/libpackage/resilience/cache`
- `github.com/platformcore/libpackage/resilience/circuit`
- `github.com/platformcore/libpackage/resilience/pagination`
- `github.com/platformcore/libpackage/resilience/retry`
- `github.com/platformcore/libpackage/resilience/sanitizer`
- `github.com/platformcore/libpackage/resilience/schema`
- `github.com/platformcore/libpackage/resilience/serializer`
- `github.com/platformcore/libpackage/resilience/validate`
- `github.com/platformcore/libpackage/resilience/validator`
- `github.com/platformcore/libpackage/foundation/runtime/health`
- `github.com/platformcore/libpackage/foundation/runtime/lifecycle`
- `github.com/platformcore/libpackage/foundation/runtime/shutdown`
- `github.com/platformcore/libpackage/foundation/runtime/signals`
- `github.com/platformcore/libpackage/observability/telemetry/baggage`
- `github.com/platformcore/libpackage/observability/telemetry/correlation`
- `github.com/platformcore/libpackage/observability/telemetry/trace`
- `github.com/platformcore/libpackage/testing/fixtures`
- `github.com/platformcore/libpackage/testing/mocks`
- `github.com/platformcore/libpackage/testing/testutil`
- `github.com/platformcore/libpackage/foundation/validator/binding`
- `github.com/platformcore/libpackage/foundation/validator/schema`

## Install Examples

```bash
go get github.com/platformcore/libpackage/foundation/client@latest
go get github.com/platformcore/libpackage/foundation/contracts@latest
go get github.com/platformcore/libpackage/foundation/core@latest
go get github.com/platformcore/libpackage/foundation/client/grpc@latest
go get github.com/platformcore/libpackage/foundation/client/http@latest
go get github.com/platformcore/libpackage/foundation/client/nats@latest
```



