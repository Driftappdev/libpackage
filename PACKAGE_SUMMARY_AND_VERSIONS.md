# LIBPACKAGE Module Summary

Updated: 2026-04-28

## Versioning Policy
- `v1.0.0`: production-grade share modules (core product-facing domains).
- `v0.1.0`: internal/support modules (faster change cadence).
- Go baseline follows each module `go.mod`.

## Module Inventory
| Path | Module | Package Version | Go | Go Files | Purpose | Suitable For |
|---|---|---:|---:|---:|---|---|
| compat/goauth | `github.com/driftappdev/compat/goauth` | v0.1.0 | 1.25.0 | 1 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| compat/gocircuit | `github.com/driftappdev/compat/gocircuit` | v0.1.0 | 1.25.0 | 1 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| compat/goerror | `github.com/driftappdev/compat/goerror` | v0.1.0 | 1.25.0 | 1 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| compat/gologger | `github.com/driftappdev/compat/gologger` | v0.1.0 | 1.25.0 | 1 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| compat/gometrics | `github.com/driftappdev/compat/gometrics` | v0.1.0 | 1.25.0 | 1 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| compat/goratelimit | `github.com/driftappdev/compat/goratelimit` | v0.1.0 | 1.25.0 | 6 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| compat/goretry | `github.com/driftappdev/compat/goretry` | v0.1.0 | 1.25.0 | 1 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| compat/gosanitizer | `github.com/driftappdev/compat/gosanitizer` | v0.1.0 | 1.25.0 | 1 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| compat/gotimeout | `github.com/driftappdev/compat/gotimeout` | v0.1.0 | 1.25.0 | 1 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| compat/gotracing | `github.com/driftappdev/compat/gotracing` | v0.1.0 | 1.25.0 | 1 | Compatibility adapters for legacy/bridge integration. | Use when this domain capability is needed as a reusable shared package. |
| foundation/client | `github.com/driftappdev/foundation/client` | v0.1.0 | 1.25.0 | 0 | Core foundation: contracts/config/runtime/schema/validation. | Use when this domain capability is needed as a reusable shared package. |
| foundation/config | `github.com/driftappdev/foundation/config` | v0.1.0 | 1.25.0 | 0 | Core foundation: contracts/config/runtime/schema/validation. | Use when this domain capability is needed as a reusable shared package. |
| foundation/contracts | `github.com/driftappdev/foundation/contracts` | v0.1.0 | 1.25.0 | 0 | Core foundation: contracts/config/runtime/schema/validation. | Use when this domain capability is needed as a reusable shared package. |
| foundation/core | `github.com/driftappdev/foundation/core` | v0.1.0 | 1.25.0 | 0 | Core foundation: contracts/config/runtime/schema/validation. | Use when this domain capability is needed as a reusable shared package. |
| foundation/result | `github.com/driftappdev/foundation/result` | v0.1.0 | 1.25.0 | 1 | Core foundation: contracts/config/runtime/schema/validation. | Use when this domain capability is needed as a reusable shared package. |
| foundation/runtime | `github.com/driftappdev/foundation/runtime` | v0.1.0 | 1.25.0 | 0 | Core foundation: contracts/config/runtime/schema/validation. | Use when this domain capability is needed as a reusable shared package. |
| foundation/schema | `github.com/driftappdev/foundation/schema` | v0.1.0 | 1.25.0 | 1 | Core foundation: contracts/config/runtime/schema/validation. | Use when this domain capability is needed as a reusable shared package. |
| foundation/validator/binding | `github.com/driftappdev/foundation/validator/binding` | v0.1.0 | 1.25.0 | 2 | Core foundation: contracts/config/runtime/schema/validation. | Use when this domain capability is needed as a reusable shared package. |
| foundation/validator/schema | `github.com/driftappdev/foundation/validator/schema` | v0.1.0 | 1.25.0 | 3 | Core foundation: contracts/config/runtime/schema/validation. | Use when this domain capability is needed as a reusable shared package. |
| messaging/audit | `github.com/driftappdev/messaging/audit` | v0.1.0 | 1.25.0 | 1 | Messaging support modules (audit/idempotency/examples). | Use when this domain capability is needed as a reusable shared package. |
| messaging/dlq | `github.com/driftappdev/messaging/dlq` | v0.1.0 | 1.25.0 | 1 | Messaging support modules (audit/idempotency/examples). | Use when this domain capability is needed as a reusable shared package. |
| messaging/example_integration | `github.com/driftappdev/messaging/example_integration` | v0.1.0 | 1.25.0 | 1 | Messaging support modules (audit/idempotency/examples). | Use when this domain capability is needed as a reusable shared package. |
| messaging/idempotency | `github.com/driftappdev/messaging/idempotency` | v0.1.0 | 1.25.0 | 1 | Messaging support modules (audit/idempotency/examples). | Use when this domain capability is needed as a reusable shared package. |
| messaging/inbox | `github.com/driftappdev/messaging/inbox` | v0.1.0 | 1.25.0 | 1 | Messaging support modules (audit/idempotency/examples). | Use when this domain capability is needed as a reusable shared package. |
| messaging/outbox | `github.com/driftappdev/messaging/outbox` | v0.1.0 | 1.25.0 | 1 | Messaging support modules (audit/idempotency/examples). | Use when this domain capability is needed as a reusable shared package. |
| messaging/redrive | `github.com/driftappdev/messaging/redrive` | v0.1.0 | 1.25.0 | 1 | Messaging support modules (audit/idempotency/examples). | Use when this domain capability is needed as a reusable shared package. |
| messaging/replay | `github.com/driftappdev/messaging/replay` | v0.1.0 | 1.25.0 | 6 | Messaging support modules (audit/idempotency/examples). | Use when this domain capability is needed as a reusable shared package. |
| middleware/clock | `github.com/driftappdev/middleware/clock` | v0.1.0 | 1.25.0 | 1 | Reusable middleware helpers (ids, trace, pooling, propagation). | Use when this domain capability is needed as a reusable shared package. |
| middleware/event | `github.com/driftappdev/middleware/event` | v0.1.0 | 1.25.0 | 1 | Reusable middleware helpers (ids, trace, pooling, propagation). | Use when this domain capability is needed as a reusable shared package. |
| middleware/ids | `github.com/driftappdev/middleware/ids` | v0.1.0 | 1.25.0 | 1 | Reusable middleware helpers (ids, trace, pooling, propagation). | Use when this domain capability is needed as a reusable shared package. |
| middleware/logging | `github.com/driftappdev/middleware/logging` | v0.1.0 | 1.25.0 | 1 | Reusable middleware helpers (ids, trace, pooling, propagation). | Use when this domain capability is needed as a reusable shared package. |
| middleware/pool | `github.com/driftappdev/middleware/pool` | v0.1.0 | 1.25.0 | 1 | Reusable middleware helpers (ids, trace, pooling, propagation). | Use when this domain capability is needed as a reusable shared package. |
| middleware/propagation | `github.com/driftappdev/middleware/propagation` | v0.1.0 | 1.25.0 | 1 | Reusable middleware helpers (ids, trace, pooling, propagation). | Use when this domain capability is needed as a reusable shared package. |
| middleware/registry | `github.com/driftappdev/middleware/registry` | v0.1.0 | 1.25.0 | 1 | Reusable middleware helpers (ids, trace, pooling, propagation). | Use when this domain capability is needed as a reusable shared package. |
| middleware/trace | `github.com/driftappdev/middleware/trace` | v0.1.0 | 1.25.0 | 1 | Reusable middleware helpers (ids, trace, pooling, propagation). | Use when this domain capability is needed as a reusable shared package. |
| observability/audit | `github.com/driftappdev/observability/audit` | v0.1.0 | 1.25.1 | 1 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/correlation | `github.com/driftappdev/observability/correlation` | v0.1.0 | 1.25.1 | 1 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability | `github.com/driftappdev/observability` | v0.1.0 | 1.25.1 | 0 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/healthcheck | `github.com/driftappdev/observability/healthcheck` | v0.1.0 | 1.25.1 | 3 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/logging | `github.com/driftappdev/observability/logging` | v0.1.0 | 1.25.1 | 3 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/metrics | `github.com/driftappdev/observability/metrics` | v0.1.0 | 1.25.1 | 2 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/metrics/registry | `github.com/driftappdev/observability/metrics/registry` | v0.1.0 | 1.25.1 | 1 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/performance | `github.com/driftappdev/observability/performance` | v0.1.0 | 1.25.1 | 1 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/profiler | `github.com/driftappdev/observability/profiler` | v0.1.0 | 1.25.1 | 2 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/sentinelprofiler | `github.com/driftappdev/observability/sentinelprofiler` | v0.1.0 | 1.25.1 | 1 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/span | `github.com/driftappdev/observability/span` | v0.1.0 | 1.25.1 | 1 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/telemetry/baggage | `github.com/driftappdev/observability/telemetry/baggage` | v0.1.0 | 1.25.1 | 1 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/telemetry/correlation | `github.com/driftappdev/observability/telemetry/correlation` | v0.1.0 | 1.25.1 | 1 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/telemetry/trace | `github.com/driftappdev/observability/telemetry/trace` | v0.1.0 | 1.25.1 | 1 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| observability/tracing | `github.com/driftappdev/observability/tracing` | v0.1.0 | 1.25.1 | 4 | Logging, metrics, tracing, health, telemetry components. | Use to standardize logs/metrics/traces/health across all services. |
| orchestration/di | `github.com/driftappdev/orchestration/di` | v0.1.0 | 1.25.0 | 0 | DI/workflow orchestration components. | Use when this domain capability is needed as a reusable shared package. |
| orchestration/workflow | `github.com/driftappdev/orchestration/workflow` | v0.1.0 | 1.25.1 | 1 | DI/workflow orchestration components. | Use when this domain capability is needed as a reusable shared package. |
| persistence/distlock | `github.com/driftappdev/persistence/distlock` | v0.1.0 | 1.25.0 | 1 | Persistence patterns (transaction, unit-of-work, distributed lock). | Use when this domain capability is needed as a reusable shared package. |
| persistence/tx | `github.com/driftappdev/persistence/tx` | v0.1.0 | 1.25.0 | 1 | Persistence patterns (transaction, unit-of-work, distributed lock). | Use when this domain capability is needed as a reusable shared package. |
| persistence/uow | `github.com/driftappdev/persistence/uow` | v0.1.0 | 1.25.0 | 1 | Persistence patterns (transaction, unit-of-work, distributed lock). | Use when this domain capability is needed as a reusable shared package. |
| platform/client | `github.com/driftappdev/platform/client` | v0.1.0 | 1.25.0 | 1 | General reusable shared module. | Use when this domain capability is needed as a reusable shared package. |
| platform/container | `github.com/driftappdev/platform/container` | v0.1.0 | 1.25.0 | 1 | General reusable shared module. | Use when this domain capability is needed as a reusable shared package. |
| platform/evaluator | `github.com/driftappdev/platform/evaluator` | v0.1.0 | 1.25.0 | 1 | General reusable shared module. | Use when this domain capability is needed as a reusable shared package. |
| platform/eventbus | `github.com/driftappdev/platform/eventbus` | v0.1.0 | 1.25.1 | 0 | General reusable shared module. | Use when this domain capability is needed as a reusable shared package. |
| platform/featureflag | `github.com/driftappdev/platform/featureflag` | v0.1.0 | 1.25.0 | 0 | General reusable shared module. | Use when this domain capability is needed as a reusable shared package. |
| platform/servicemesh | `github.com/driftappdev/platform/servicemesh` | v0.1.0 | 1.25.0 | 1 | General reusable shared module. | Use when this domain capability is needed as a reusable shared package. |
| platform/versioning | `github.com/driftappdev/platform/versioning` | v0.1.0 | 1.25.0 | 1 | General reusable shared module. | Use when this domain capability is needed as a reusable shared package. |
| plugins/common | `github.com/driftappdev/plugins/common` | v0.1.0 | 1.25.1 | 1 | Plugin runtime: manifest, loader, registry, hooks, providers. | Use to add extensibility points and dynamic integrations. |
| plugins/engine | `github.com/driftappdev/plugins/engine` | v0.1.0 | 1.25.1 | 1 | Plugin runtime: manifest, loader, registry, hooks, providers. | Use to add extensibility points and dynamic integrations. |
| plugins | `github.com/driftappdev/plugins` | v0.1.0 | 1.25.1 | 0 | Plugin runtime: manifest, loader, registry, hooks, providers. | Use to add extensibility points and dynamic integrations. |
| plugins/hooks | `github.com/driftappdev/plugins/hooks` | v0.1.0 | 1.25.1 | 1 | Plugin runtime: manifest, loader, registry, hooks, providers. | Use to add extensibility points and dynamic integrations. |
| plugins/loader | `github.com/driftappdev/plugins/loader` | v0.1.0 | 1.25.1 | 1 | Plugin runtime: manifest, loader, registry, hooks, providers. | Use to add extensibility points and dynamic integrations. |
| plugins/manifest | `github.com/driftappdev/plugins/manifest` | v0.1.0 | 1.25.1 | 1 | Plugin runtime: manifest, loader, registry, hooks, providers. | Use to add extensibility points and dynamic integrations. |
| plugins/provider | `github.com/driftappdev/plugins/provider` | v0.1.0 | 1.25.1 | 1 | Plugin runtime: manifest, loader, registry, hooks, providers. | Use to add extensibility points and dynamic integrations. |
| plugins/registry | `github.com/driftappdev/plugins/registry` | v0.1.0 | 1.25.1 | 1 | Plugin runtime: manifest, loader, registry, hooks, providers. | Use to add extensibility points and dynamic integrations. |
| ratelimit | `github.com/driftappdev/ratelimit` | v1.0.0 | 1.25.1 | 3 | Rate limiting domain library for API/service protection. | Use to enforce quota policies by route/tenant/client. |
| resilience/adaptive_concurrency | `github.com/driftappdev/resilience/adaptive_concurrency` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/adaptive_retry | `github.com/driftappdev/resilience/adaptive_retry` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/backpressure | `github.com/driftappdev/resilience/backpressure` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/batcher | `github.com/driftappdev/resilience/batcher` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/bulkhead | `github.com/driftappdev/resilience/bulkhead` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/cache_stampede_protection | `github.com/driftappdev/resilience/cache_stampede_protection` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/checkpoint | `github.com/driftappdev/resilience/checkpoint` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/congestion_control | `github.com/driftappdev/resilience/congestion_control` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/deadline | `github.com/driftappdev/resilience/deadline` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/distributed_rate_limit | `github.com/driftappdev/resilience/distributed_rate_limit` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience | `github.com/driftappdev/resilience` | v0.1.0 | 1.25.1 | 0 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/health_supervisor | `github.com/driftappdev/resilience/health_supervisor` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/hedged_request | `github.com/driftappdev/resilience/hedged_request` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/limiter | `github.com/driftappdev/resilience/limiter` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/load_shedder | `github.com/driftappdev/resilience/load_shedder` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/priority_queue | `github.com/driftappdev/resilience/priority_queue` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/quorum | `github.com/driftappdev/resilience/quorum` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/request_coalescing | `github.com/driftappdev/resilience/request_coalescing` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/retry_backoff | `github.com/driftappdev/resilience/retry_backoff` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/shadow_traffic | `github.com/driftappdev/resilience/shadow_traffic` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/state_synchronizer | `github.com/driftappdev/resilience/state_synchronizer` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/token_bucket | `github.com/driftappdev/resilience/token_bucket` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/traffic_mirroring | `github.com/driftappdev/resilience/traffic_mirroring` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/validator | `github.com/driftappdev/resilience/validator` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| resilience/workqueue | `github.com/driftappdev/resilience/workqueue` | v0.1.0 | 1.25.1 | 1 | Resilience patterns: retry, bulkhead, limiter, load-shedding, quorum, etc. | Use to harden service behavior during failures and traffic spikes. |
| security | `github.com/driftappdev/security` | v1.0.0 | 1.25.1 | 2 | Security utilities (JWT/policy/permission/threat-defense). | Use for shared authn/authz/policy enforcement. |
| tools/lib_word | `github.com/driftappdev/tools/lib_word` | v0.1.0 | 1.25.0 | 0 | Developer tooling utilities. | Use when this domain capability is needed as a reusable shared package. |

## Release Guidance
- Publish per-module tags (example: `platform/eventbus/v1.0.0`).
- Keep backward compatibility for `v1.x` modules.
- Increase major version only on breaking changes.


