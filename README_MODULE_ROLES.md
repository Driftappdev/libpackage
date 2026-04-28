# Libpackage Module Roles

à¹€à¸­à¸à¸ªà¸²à¸£à¸™à¸µà¹‰à¸ªà¸£à¸¸à¸›à¸«à¸™à¹‰à¸²à¸—à¸µà¹ˆà¸‚à¸­à¸‡à¸—à¸¸à¸à¹‚à¸¡à¸”à¸¹à¸¥à¸—à¸µà¹ˆà¸žà¸šà¸ˆà¸²à¸à¹„à¸Ÿà¸¥à¹Œ `go.mod` à¹ƒà¸•à¹‰ `libpackage` à¹à¸¥à¸°à¹€à¸Šà¹‡à¸à¸šà¸—à¸šà¸²à¸—à¸‹à¹‰à¸³ (overlap) à¹ƒà¸™à¸£à¸°à¸”à¸±à¸šà¸ªà¸–à¸²à¸›à¸±à¸•à¸¢à¸à¸£à¸£à¸¡

## 1) Foundation / Shared
- `github.com/driftappdev`: root aggregate module
- `github.com/driftappdev/foundation/core`: primitive à¸à¸¥à¸²à¸‡ (errors/logger/types/context/utils/result)
- `github.com/driftappdev/foundation/contracts`: à¸ªà¸±à¸à¸à¸² DTO/response/pagination/versioning
- `github.com/driftappdev/foundation/config`: config loading / env / defaults
- `github.com/driftappdev/foundation/result`: generic result envelope
- `github.com/driftappdev/foundation/runtime`: lifecycle/health/shutdown/runtime helpers
- `github.com/driftappdev/foundation/validator`: validation facade
- `github.com/driftappdev/foundation/client`: client abstractions

## 2) Dependency Injection
- `github.com/driftappdev/orchestration/di`: DI umbrella
- `github.com/driftappdev/orchestration/di/container`: container
- `github.com/driftappdev/orchestration/di/module`: module registration
- `github.com/driftappdev/orchestration/di/provider`: provider wiring
- `github.com/driftappdev/orchestration/di/registry`: DI registry
- `github.com/driftappdev/orchestration/di/scope`: scope/lifetime

## 3) Messaging
- `github.com/driftappdev/messaging/inbox`: consumer-side intake queue
- `github.com/driftappdev/messaging/outbox`: producer-side outbox dispatch
- `github.com/driftappdev/messaging/dlq`: dead-letter queue
- `github.com/driftappdev/messaging/redrive`: DLQ redrive/replay to target
- `github.com/driftappdev/messaging/replay`: replay orchestration
- `github.com/driftappdev/messaging/idempotency`: idempotency guard
- `github.com/driftappdev/messaging/example_integration`: adapter/integration example
- `github.com/driftappdev/messaging/audit`: messaging audit events

## 4) Security
- `github.com/driftappdev/security`: security umbrella
- `github.com/driftappdev/security/jwt`: JWT
- `github.com/driftappdev/security/oauth2`: OAuth2
- `github.com/driftappdev/security/hash`: hashing
- `github.com/driftappdev/security/encryption`: encryption
- `github.com/driftappdev/security/permission`: permission checks
- `github.com/driftappdev/security/policy`: policy checks
- `github.com/driftappdev/security/threatdefense`: threat defense rules

## 5) Persistence
- `github.com/driftappdev/persistence`: persistence umbrella
- `github.com/driftappdev/persistence/tx`: transaction helpers
- `github.com/driftappdev/persistence/uow`: unit-of-work
- `github.com/driftappdev/persistence/distlock`: distributed lock

## 6) Observability / Telemetry
- `github.com/driftappdev/observability`: observability umbrella
- `github.com/driftappdev/observability/correlation`: correlation helpers
- `github.com/driftappdev/observability/healthcheck`: liveness/readiness/health
- `github.com/driftappdev/observability/span`: span helpers
- `github.com/driftappdev/observability/tracing`: tracing helpers
- `github.com/driftappdev/observability/performance`: performance metrics helpers
- `github.com/driftappdev/observability/sentinelprofiler`: profiler integration
- `github.com/driftappdev/observability/audit`: observability-side audit
- `github.com/driftappdev/observability/telemetry`: telemetry umbrella (OpenTelemetry-centric)

## 7) Platform / Plugins / Eventing
- `github.com/driftappdev/platform`: platform umbrella
- `github.com/driftappdev/platform/servicemesh`: service mesh integration
- `github.com/driftappdev/plugins`: plugin umbrella
- `github.com/driftappdev/plugins/common`: shared plugin contracts/util
- `github.com/driftappdev/plugins/engine`: plugin engine/runtime
- `github.com/driftappdev/platform/eventbus`: event bus abstractions

## 8) Resilience / Rate limiting
- `github.com/driftappdev/resilience`: resilience umbrella
- `github.com/driftappdev/resilience/retry`: retry
- `github.com/driftappdev/resilience/sanitizer`: sanitize/cleanup
- `github.com/driftappdev/resilience/validate`: validation utilities
- `github.com/driftappdev/resilience/validator`: validator helpers
- `github.com/driftappdev/resilience/pagination`: pagination helpers
- `github.com/driftappdev/resilience/cache`: cache resilience helpers
- `github.com/driftappdev/resilience/circuit`: circuit breaker
- `github.com/driftappdev/ratelimit`: rate limit umbrella
- `github.com/driftappdev/ratelimit/memory_store`: memory store for rate limit
- `github.com/driftappdev/ratelimit/enterprise`: enterprise rate-limit features

## 9) Middleware
- `github.com/driftappdev/middleware`: middleware umbrella
- `github.com/driftappdev/middleware/requestid`: request id middleware
- `github.com/driftappdev/middleware/timeout`: timeout middleware
- `github.com/driftappdev/middleware/logmid/logging-middleware`: logging middleware variant
- `github.com/driftappdev/middleware/adminshield/admin-middleware`: admin shield middleware variant
- `github.com/driftappdev/observability/logging-middleware`: standalone logging middleware
- `github.com/driftappdev/adminshield`: standalone adminshield (namespace à¸žà¸´à¸¡à¸žà¹Œà¸•à¹ˆà¸²à¸‡à¸ˆà¸²à¸ driftappdev)
- `github.com/driftappdev/auth`: standalone auth (namespace à¸žà¸´à¸¡à¸žà¹Œà¸•à¹ˆà¸²à¸‡à¸ˆà¸²à¸ driftappdev)

## 10) Legacy / Compatibility Facades (`go*`)
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

## 11) Folder Alias Map (à¸Šà¸·à¹ˆà¸­à¹‚à¸Ÿà¸¥à¹€à¸”à¸­à¸£à¹Œ != module path)
à¸à¸¥à¸¸à¹ˆà¸¡à¸™à¸µà¹‰à¹„à¸¡à¹ˆà¹ƒà¸Šà¹ˆà¹‚à¸¡à¸”à¸¹à¸¥à¹€à¸žà¸´à¹ˆà¸¡ à¹à¸•à¹ˆà¹€à¸›à¹‡à¸™à¹‚à¸Ÿà¸¥à¹€à¸”à¸­à¸£à¹Œà¸—à¸µà¹ˆà¸Šà¸µà¹‰à¹„à¸›à¸¢à¸±à¸‡ module path à¹€à¸Šà¸´à¸‡à¹‚à¸”à¹€à¸¡à¸™:
- `audit` -> `github.com/driftappdev/observability/audit`
- `auditv.1` -> `github.com/driftappdev/messaging/audit`
- `cache` -> `github.com/driftappdev/resilience/cache`
- `circuit` -> `github.com/driftappdev/resilience/circuit`
- `retry` -> `github.com/driftappdev/resilience/retry`
- `sanitizer` -> `github.com/driftappdev/resilience/sanitizer`
- `pagination` -> `github.com/driftappdev/resilience/pagination`
- `dlq` -> `github.com/driftappdev/messaging/dlq`
- `inbox` -> `github.com/driftappdev/messaging/inbox`
- `outbox` -> `github.com/driftappdev/messaging/outbox`
- `redrive` -> `github.com/driftappdev/messaging/redrive`
- `replay` -> `github.com/driftappdev/messaging/replay`
- `jwt` -> `github.com/driftappdev/security/jwt`
- `oauth2` -> `github.com/driftappdev/security/oauth2`
- `hash` -> `github.com/driftappdev/security/hash`
- `encryption` -> `github.com/driftappdev/security/encryption`
- `permission` -> `github.com/driftappdev/security/permission`
- `policy` -> `github.com/driftappdev/security/policy`
- `threatdefense` -> `github.com/driftappdev/security/threatdefense`
- `distlock` -> `github.com/driftappdev/persistence/distlock`
- `tx` -> `github.com/driftappdev/persistence/tx`
- `uow` -> `github.com/driftappdev/persistence/uow`
- `servicemesh` -> `github.com/driftappdev/platform/servicemesh`
- `common` -> `github.com/driftappdev/plugins/common`
- `engine` -> `github.com/driftappdev/plugins/engine`
- `enterprise` -> `github.com/driftappdev/ratelimit/enterprise`
- `performance` -> `github.com/driftappdev/observability/performance`
- `sentinelprofiler` -> `github.com/driftappdev/observability/sentinelprofiler`
- `ratelimitX` -> `github.com/driftappdev/ratelimit`

---

## Overlap Check (à¸šà¸—à¸šà¸²à¸—à¸‹à¹‰à¸³)

### A) à¸‹à¹‰à¸³à¹à¸šà¸š â€œAlias Moduleâ€
à¸à¸¥à¸¸à¹ˆà¸¡à¸™à¸µà¹‰à¸‹à¹‰à¸³à¸šà¸—à¸šà¸²à¸—à¹‚à¸”à¸¢à¸•à¸±à¹‰à¸‡à¹ƒà¸ˆ (à¸Šà¸·à¹ˆà¸­à¸ªà¸±à¹‰à¸™ vs à¸Šà¸·à¹ˆà¸­à¹‚à¸”à¹€à¸¡à¸™à¹€à¸•à¹‡à¸¡):
- `inbox/outbox/dlq/redrive/replay` vs `messaging/*`
- `retry/sanitizer/cache/circuit/pagination` vs `resilience/*`
- `jwt/oauth2/hash/encryption/permission/policy/threatdefense` vs `security/*`
- `servicemesh` vs `platform/servicemesh`
- `distlock/tx/uow` vs `persistence/*`
- `audit/performance/sentinelprofiler` vs `observability/*`
- `ratelimitX` vs `ratelimit`

### B) à¸‹à¹‰à¸³à¹€à¸Šà¸´à¸‡à¹‚à¸”à¹€à¸¡à¸™ (à¹„à¸¡à¹ˆà¸ˆà¸³à¹€à¸›à¹‡à¸™à¸•à¹‰à¸­à¸‡à¸¡à¸µà¸—à¸±à¹‰à¸‡à¸„à¸¹à¹ˆ)
- `telemetry` vs `observability/*` (tracing/metrics overlap à¸ªà¸¹à¸‡)
- `plugins/*` vs `platform/*` (loader/registry/provider/hooks overlap)
- `validator` vs `resilience/validate` vs `resilience/validator`
- `logging-middleware` vs `middleware/logmid/logging-middleware`
- `auditv.1 (messaging/audit)` vs `audit (observability/audit)` à¸•à¹‰à¸­à¸‡à¹à¸¢à¸à¸‚à¸­à¸šà¹€à¸‚à¸•à¹ƒà¸«à¹‰à¸Šà¸±à¸”

### C) à¸ˆà¸¸à¸”à¹€à¸ªà¸µà¹ˆà¸¢à¸‡à¸„à¸§à¸²à¸¡à¹„à¸¡à¹ˆà¸ªà¸¡à¹ˆà¸³à¹€à¸ªà¸¡à¸­
- namespace à¸ªà¸°à¸à¸”à¸•à¹ˆà¸²à¸‡à¸à¸±à¸™: `github.com/diftappdev/...` vs `github.com/driftappdev/...`
- à¸šà¸²à¸‡à¹‚à¸¡à¸”à¸¹à¸¥à¸¢à¸±à¸‡à¹€à¸›à¹‡à¸™ legacy alias à¸ˆà¸³à¸™à¸§à¸™à¸¡à¸²à¸ à¸—à¸³à¹ƒà¸«à¹‰à¸”à¸¹à¹€à¸«à¸¡à¸·à¸­à¸™à¸‹à¹‰à¸³à¹€à¸¢à¸­à¸°à¸à¸§à¹ˆà¸²à¸„à¸§à¸²à¸¡à¸ˆà¸£à¸´à¸‡

## Recommended Direction
- à¸–à¹‰à¸²à¸•à¹‰à¸­à¸‡à¸à¸²à¸£à¸¥à¸”à¸‹à¹‰à¸³: à¹€à¸¥à¸·à¸­à¸ â€œcanonical pathâ€ à¸•à¹ˆà¸­à¹‚à¸”à¹€à¸¡à¸™à¸¥à¸° 1 à¸Šà¸¸à¸” (à¹€à¸Šà¹ˆà¸™ `messaging/*`, `security/*`, `resilience/*`, `observability/*`, `platform/*`) à¹à¸¥à¹‰à¸§à¸„à¸‡ alias à¹€à¸‰à¸žà¸²à¸°à¸—à¸µà¹ˆà¸ˆà¸³à¹€à¸›à¹‡à¸™à¸•à¹ˆà¸­ backward compatibility
- à¸à¸³à¸«à¸™à¸” policy à¸Šà¸±à¸”à¹€à¸ˆà¸™: module à¹ƒà¸«à¸¡à¹ˆà¸•à¹‰à¸­à¸‡à¹€à¸‚à¹‰à¸²à¸Šà¸¸à¸” canonical à¸à¹ˆà¸­à¸™à¹€à¸ªà¸¡à¸­
- à¹à¸à¹‰ namespace à¹ƒà¸«à¹‰à¹€à¸«à¸¥à¸·à¸­ `driftappdev` à¹à¸šà¸šà¹€à¸”à¸µà¸¢à¸§à¸—à¸±à¹‰à¸‡ repo




