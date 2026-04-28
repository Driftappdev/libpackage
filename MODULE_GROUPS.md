# Module Structure (Mainstream Style)

This repository follows a domain-first Go module layout:
- Main module = domain root (can be installed alone)
- Submodule = optional focused package under that domain
- Submodules are not required unless imported by your app or required by the main module

## Main Modules (With Submodules)

- `github.com/driftappdev/foundation/client`
- `github.com/driftappdev/foundation/contracts`
- `github.com/driftappdev/foundation/core`
- `github.com/driftappdev/orchestration/di`
- `github.com/driftappdev/platform/eventbus`
- `github.com/driftappdev/platform/featureflag`
- `github.com/driftappdev/infra`
- `github.com/driftappdev/observability`
- `github.com/driftappdev/platform`
- `github.com/driftappdev/plugins`
- `github.com/driftappdev/resilience`
- `github.com/driftappdev/foundation/runtime`
- `github.com/driftappdev/observability/telemetry`
- `github.com/driftappdev/testing`
- `github.com/driftappdev/foundation/validator`

## Standalone Main Modules (No Submodules)

- `github.com/driftappdev/foundation/config`
- `github.com/driftappdev/docs`
- `github.com/driftappdev/compat/goauth`
- `github.com/driftappdev/compat/gocircuit`
- `github.com/driftappdev/compat/goerror`
- `github.com/driftappdev/compat/gologger`
- `github.com/driftappdev/compat/gometrics`
- `github.com/driftappdev/compat/goratelimit`
- `github.com/driftappdev/compat/goretry`
- `github.com/driftappdev/compat/gosanitizer`
- `github.com/driftappdev/compat/gotimeout`
- `github.com/driftappdev/compat/gotracing`
- `github.com/driftappdev/logmid/logging-middleware`
- `github.com/driftappdev/ratelimit`
- `github.com/driftappdev/foundation/result`
- `github.com/driftappdev/security/encryption`
- `github.com/driftappdev/security/hash`
- `github.com/driftappdev/security/jwt`
- `github.com/driftappdev/security/oauth2`
- `github.com/driftappdev/security/secrets`

## Submodules

- `github.com/driftappdev/foundation/client/grpc`
- `github.com/driftappdev/foundation/client/http`
- `github.com/driftappdev/foundation/client/nats`
- `github.com/driftappdev/foundation/contracts/errors`
- `github.com/driftappdev/foundation/contracts/pagination`
- `github.com/driftappdev/foundation/contracts/response`
- `github.com/driftappdev/foundation/contracts/versioning`
- `github.com/driftappdev/foundation/core/constants`
- `github.com/driftappdev/foundation/core/context`
- `github.com/driftappdev/foundation/core/errors`
- `github.com/driftappdev/foundation/core/logger`
- `github.com/driftappdev/foundation/core/result`
- `github.com/driftappdev/foundation/core/types`
- `github.com/driftappdev/foundation/core/utils`
- `github.com/driftappdev/orchestration/di/container`
- `github.com/driftappdev/orchestration/di/module`
- `github.com/driftappdev/orchestration/di/provider`
- `github.com/driftappdev/orchestration/di/registry`
- `github.com/driftappdev/orchestration/di/scope`
- `github.com/driftappdev/platform/eventbus/deadletter`
- `github.com/driftappdev/platform/eventbus/envelope`
- `github.com/driftappdev/platform/eventbus/headers`
- `github.com/driftappdev/platform/eventbus/idempotency`
- `github.com/driftappdev/platform/eventbus/publisher`
- `github.com/driftappdev/platform/eventbus/registry`
- `github.com/driftappdev/platform/eventbus/retry`
- `github.com/driftappdev/platform/eventbus/serializer`
- `github.com/driftappdev/platform/eventbus/subscriber`
- `github.com/driftappdev/platform/featureflag/cache`
- `github.com/driftappdev/platform/featureflag/client`
- `github.com/driftappdev/platform/featureflag/evaluator`
- `github.com/driftappdev/platform/featureflag/provider`
- `github.com/driftappdev/platform/featureflag/types`
- `github.com/driftappdev/infra/backoff`
- `github.com/driftappdev/infra/bulkhead`
- `github.com/driftappdev/infra/cache`
- `github.com/driftappdev/infra/circuit`
- `github.com/driftappdev/infra/clock`
- `github.com/driftappdev/infra/retry`
- `github.com/driftappdev/observability/correlation`
- `github.com/driftappdev/observability/healthcheck`
- `github.com/driftappdev/observability/logging`
- `github.com/driftappdev/observability/profiler`
- `github.com/driftappdev/observability/span`
- `github.com/driftappdev/observability/trace`
- `github.com/driftappdev/observability/tracing`
- `github.com/driftappdev/platform/client`
- `github.com/driftappdev/platform/container`
- `github.com/driftappdev/platform/evaluator`
- `github.com/driftappdev/platform/hooks`
- `github.com/driftappdev/platform/loader`
- `github.com/driftappdev/platform/provider`
- `github.com/driftappdev/platform/registry`
- `github.com/driftappdev/platform/versioning`
- `github.com/driftappdev/plugins/hooks`
- `github.com/driftappdev/plugins/loader`
- `github.com/driftappdev/plugins/manifest`
- `github.com/driftappdev/plugins/registry`
- `github.com/driftappdev/resilience/cache`
- `github.com/driftappdev/resilience/circuit`
- `github.com/driftappdev/resilience/pagination`
- `github.com/driftappdev/resilience/retry`
- `github.com/driftappdev/resilience/sanitizer`
- `github.com/driftappdev/resilience/schema`
- `github.com/driftappdev/resilience/serializer`
- `github.com/driftappdev/resilience/validate`
- `github.com/driftappdev/resilience/validator`
- `github.com/driftappdev/foundation/runtime/health`
- `github.com/driftappdev/foundation/runtime/lifecycle`
- `github.com/driftappdev/foundation/runtime/shutdown`
- `github.com/driftappdev/foundation/runtime/signals`
- `github.com/driftappdev/observability/telemetry/baggage`
- `github.com/driftappdev/observability/telemetry/correlation`
- `github.com/driftappdev/observability/telemetry/trace`
- `github.com/driftappdev/testing/fixtures`
- `github.com/driftappdev/testing/mocks`
- `github.com/driftappdev/testing/testutil`
- `github.com/driftappdev/foundation/validator/binding`
- `github.com/driftappdev/foundation/validator/schema`

## Install Examples

```bash
go get github.com/driftappdev/foundation/client@latest
go get github.com/driftappdev/foundation/contracts@latest
go get github.com/driftappdev/foundation/core@latest
go get github.com/driftappdev/foundation/client/grpc@latest
go get github.com/driftappdev/foundation/client/http@latest
go get github.com/driftappdev/foundation/client/nats@latest
```



