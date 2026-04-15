# Module Structure (Mainstream Style)

This repository follows a domain-first Go module layout:
- Main module = domain root (can be installed alone)
- Submodule = optional focused package under that domain
- Submodules are not required unless imported by your app or required by the main module

## Main Modules (With Submodules)

- `github.com/driftappdev/libpackage/client`
- `github.com/driftappdev/libpackage/contracts`
- `github.com/driftappdev/libpackage/core`
- `github.com/driftappdev/libpackage/di`
- `github.com/driftappdev/libpackage/eventbus`
- `github.com/driftappdev/libpackage/featureflag`
- `github.com/driftappdev/libpackage/infra`
- `github.com/driftappdev/libpackage/observability`
- `github.com/driftappdev/libpackage/platform`
- `github.com/driftappdev/libpackage/plugins`
- `github.com/driftappdev/libpackage/resilience`
- `github.com/driftappdev/libpackage/runtime`
- `github.com/driftappdev/libpackage/telemetry`
- `github.com/driftappdev/libpackage/testing`
- `github.com/driftappdev/libpackage/validator`

## Standalone Main Modules (No Submodules)

- `github.com/driftappdev/libpackage/config`
- `github.com/driftappdev/libpackage/docs`
- `github.com/driftappdev/libpackage/goauth`
- `github.com/driftappdev/libpackage/gocircuit`
- `github.com/driftappdev/libpackage/goerror`
- `github.com/driftappdev/libpackage/gologger`
- `github.com/driftappdev/libpackage/gometrics`
- `github.com/driftappdev/libpackage/goratelimit`
- `github.com/driftappdev/libpackage/goretry`
- `github.com/driftappdev/libpackage/gosanitizer`
- `github.com/driftappdev/libpackage/gotimeout`
- `github.com/driftappdev/libpackage/gotracing`
- `github.com/driftappdev/libpackage/logmid/logging-middleware`
- `github.com/driftappdev/libpackage/ratelimit`
- `github.com/driftappdev/libpackage/result`
- `github.com/driftappdev/libpackage/security/encryption`
- `github.com/driftappdev/libpackage/security/hash`
- `github.com/driftappdev/libpackage/security/jwt`
- `github.com/driftappdev/libpackage/security/oauth2`
- `github.com/driftappdev/libpackage/security/secrets`

## Submodules

- `github.com/driftappdev/libpackage/client/grpc`
- `github.com/driftappdev/libpackage/client/http`
- `github.com/driftappdev/libpackage/client/nats`
- `github.com/driftappdev/libpackage/contracts/errors`
- `github.com/driftappdev/libpackage/contracts/pagination`
- `github.com/driftappdev/libpackage/contracts/response`
- `github.com/driftappdev/libpackage/contracts/versioning`
- `github.com/driftappdev/libpackage/core/constants`
- `github.com/driftappdev/libpackage/core/context`
- `github.com/driftappdev/libpackage/core/errors`
- `github.com/driftappdev/libpackage/core/logger`
- `github.com/driftappdev/libpackage/core/result`
- `github.com/driftappdev/libpackage/core/types`
- `github.com/driftappdev/libpackage/core/utils`
- `github.com/driftappdev/libpackage/di/container`
- `github.com/driftappdev/libpackage/di/module`
- `github.com/driftappdev/libpackage/di/provider`
- `github.com/driftappdev/libpackage/di/registry`
- `github.com/driftappdev/libpackage/di/scope`
- `github.com/driftappdev/libpackage/eventbus/deadletter`
- `github.com/driftappdev/libpackage/eventbus/envelope`
- `github.com/driftappdev/libpackage/eventbus/headers`
- `github.com/driftappdev/libpackage/eventbus/idempotency`
- `github.com/driftappdev/libpackage/eventbus/publisher`
- `github.com/driftappdev/libpackage/eventbus/registry`
- `github.com/driftappdev/libpackage/eventbus/retry`
- `github.com/driftappdev/libpackage/eventbus/serializer`
- `github.com/driftappdev/libpackage/eventbus/subscriber`
- `github.com/driftappdev/libpackage/featureflag/cache`
- `github.com/driftappdev/libpackage/featureflag/client`
- `github.com/driftappdev/libpackage/featureflag/evaluator`
- `github.com/driftappdev/libpackage/featureflag/provider`
- `github.com/driftappdev/libpackage/featureflag/types`
- `github.com/driftappdev/libpackage/infra/backoff`
- `github.com/driftappdev/libpackage/infra/bulkhead`
- `github.com/driftappdev/libpackage/infra/cache`
- `github.com/driftappdev/libpackage/infra/circuit`
- `github.com/driftappdev/libpackage/infra/clock`
- `github.com/driftappdev/libpackage/infra/retry`
- `github.com/driftappdev/libpackage/observability/correlation`
- `github.com/driftappdev/libpackage/observability/healthcheck`
- `github.com/driftappdev/libpackage/observability/logging`
- `github.com/driftappdev/libpackage/observability/profiler`
- `github.com/driftappdev/libpackage/observability/span`
- `github.com/driftappdev/libpackage/observability/trace`
- `github.com/driftappdev/libpackage/observability/tracing`
- `github.com/driftappdev/libpackage/platform/client`
- `github.com/driftappdev/libpackage/platform/container`
- `github.com/driftappdev/libpackage/platform/evaluator`
- `github.com/driftappdev/libpackage/platform/hooks`
- `github.com/driftappdev/libpackage/platform/loader`
- `github.com/driftappdev/libpackage/platform/provider`
- `github.com/driftappdev/libpackage/platform/registry`
- `github.com/driftappdev/libpackage/platform/versioning`
- `github.com/driftappdev/libpackage/plugins/hooks`
- `github.com/driftappdev/libpackage/plugins/loader`
- `github.com/driftappdev/libpackage/plugins/manifest`
- `github.com/driftappdev/libpackage/plugins/registry`
- `github.com/driftappdev/libpackage/resilience/cache`
- `github.com/driftappdev/libpackage/resilience/circuit`
- `github.com/driftappdev/libpackage/resilience/pagination`
- `github.com/driftappdev/libpackage/resilience/retry`
- `github.com/driftappdev/libpackage/resilience/sanitizer`
- `github.com/driftappdev/libpackage/resilience/schema`
- `github.com/driftappdev/libpackage/resilience/serializer`
- `github.com/driftappdev/libpackage/resilience/validate`
- `github.com/driftappdev/libpackage/resilience/validator`
- `github.com/driftappdev/libpackage/runtime/health`
- `github.com/driftappdev/libpackage/runtime/lifecycle`
- `github.com/driftappdev/libpackage/runtime/shutdown`
- `github.com/driftappdev/libpackage/runtime/signals`
- `github.com/driftappdev/libpackage/telemetry/baggage`
- `github.com/driftappdev/libpackage/telemetry/correlation`
- `github.com/driftappdev/libpackage/telemetry/trace`
- `github.com/driftappdev/libpackage/testing/fixtures`
- `github.com/driftappdev/libpackage/testing/mocks`
- `github.com/driftappdev/libpackage/testing/testutil`
- `github.com/driftappdev/libpackage/validator/binding`
- `github.com/driftappdev/libpackage/validator/schema`

## Install Examples

```bash
go get github.com/driftappdev/libpackage/client@latest
go get github.com/driftappdev/libpackage/contracts@latest
go get github.com/driftappdev/libpackage/core@latest
go get github.com/driftappdev/libpackage/client/grpc@latest
go get github.com/driftappdev/libpackage/client/http@latest
go get github.com/driftappdev/libpackage/client/nats@latest
```
