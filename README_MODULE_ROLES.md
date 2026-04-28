# Libpackage Module Roles

à¹€à¸­à¸à¸ªà¸²à¸£à¸™à¸µà¹‰à¸ªà¸£à¸¸à¸›à¸«à¸™à¹‰à¸²à¸—à¸µà¹ˆà¸‚à¸­à¸‡à¸—à¸¸à¸à¹‚à¸¡à¸”à¸¹à¸¥à¸—à¸µà¹ˆà¸žà¸šà¸ˆà¸²à¸à¹„à¸Ÿà¸¥à¹Œ `go.mod` à¹ƒà¸•à¹‰ `libpackage` à¹à¸¥à¸°à¹€à¸Šà¹‡à¸à¸šà¸—à¸šà¸²à¸—à¸‹à¹‰à¸³ (overlap) à¹ƒà¸™à¸£à¸°à¸”à¸±à¸šà¸ªà¸–à¸²à¸›à¸±à¸•à¸¢à¸à¸£à¸£à¸¡

## 1) Foundation / Shared
- `github.com/platformcore/libpackage`: root aggregate module
- `github.com/platformcore/libpackage/foundation/core`: primitive à¸à¸¥à¸²à¸‡ (errors/logger/types/context/utils/result)
- `github.com/platformcore/libpackage/foundation/contracts`: à¸ªà¸±à¸à¸à¸² DTO/response/pagination/versioning
- `github.com/platformcore/libpackage/foundation/config`: config loading / env / defaults
- `github.com/platformcore/libpackage/foundation/result`: generic result envelope
- `github.com/platformcore/libpackage/foundation/runtime`: lifecycle/health/shutdown/runtime helpers
- `github.com/platformcore/libpackage/foundation/validator`: validation facade
- `github.com/platformcore/libpackage/foundation/client`: client abstractions

## 2) Dependency Injection
- `github.com/platformcore/libpackage/orchestration/di`: DI umbrella
- `github.com/platformcore/libpackage/orchestration/di/container`: container
- `github.com/platformcore/libpackage/orchestration/di/module`: module registration
- `github.com/platformcore/libpackage/orchestration/di/provider`: provider wiring
- `github.com/platformcore/libpackage/orchestration/di/registry`: DI registry
- `github.com/platformcore/libpackage/orchestration/di/scope`: scope/lifetime

## 3) Messaging
- `github.com/platformcore/libpackage/messaging/inbox`: consumer-side intake queue
- `github.com/platformcore/libpackage/messaging/outbox`: producer-side outbox dispatch
- `github.com/platformcore/libpackage/messaging/dlq`: dead-letter queue
- `github.com/platformcore/libpackage/messaging/redrive`: DLQ redrive/replay to target
- `github.com/platformcore/libpackage/messaging/replay`: replay orchestration
- `github.com/platformcore/libpackage/messaging/idempotency`: idempotency guard
- `github.com/platformcore/libpackage/messaging/example_integration`: adapter/integration example
- `github.com/platformcore/libpackage/messaging/audit`: messaging audit events

## 4) Security
- `github.com/platformcore/libpackage/security`: security umbrella
- `github.com/platformcore/libpackage/security/jwt`: JWT
- `github.com/platformcore/libpackage/security/oauth2`: OAuth2
- `github.com/platformcore/libpackage/security/hash`: hashing
- `github.com/platformcore/libpackage/security/encryption`: encryption
- `github.com/platformcore/libpackage/security/permission`: permission checks
- `github.com/platformcore/libpackage/security/policy`: policy checks
- `github.com/platformcore/libpackage/security/threatdefense`: threat defense rules

## 5) Persistence
- `github.com/platformcore/libpackage/persistence`: persistence umbrella
- `github.com/platformcore/libpackage/persistence/tx`: transaction helpers
- `github.com/platformcore/libpackage/persistence/uow`: unit-of-work
- `github.com/platformcore/libpackage/persistence/distlock`: distributed lock

## 6) Observability / Telemetry
- `github.com/platformcore/libpackage/observability`: observability umbrella
- `github.com/platformcore/libpackage/observability/correlation`: correlation helpers
- `github.com/platformcore/libpackage/observability/healthcheck`: liveness/readiness/health
- `github.com/platformcore/libpackage/observability/span`: span helpers
- `github.com/platformcore/libpackage/observability/tracing`: tracing helpers
- `github.com/platformcore/libpackage/observability/performance`: performance metrics helpers
- `github.com/platformcore/libpackage/observability/sentinelprofiler`: profiler integration
- `github.com/platformcore/libpackage/observability/audit`: observability-side audit
- `github.com/platformcore/libpackage/observability/telemetry`: telemetry umbrella (OpenTelemetry-centric)

## 7) Platform / Plugins / Eventing
- `github.com/platformcore/libpackage/platform`: platform umbrella
- `github.com/platformcore/libpackage/platform/servicemesh`: service mesh integration
- `github.com/platformcore/libpackage/plugins`: plugin umbrella
- `github.com/platformcore/libpackage/plugins/common`: shared plugin contracts/util
- `github.com/platformcore/libpackage/plugins/engine`: plugin engine/runtime
- `github.com/platformcore/libpackage/platform/eventbus`: event bus abstractions

## 8) Resilience / Rate limiting
- `github.com/platformcore/libpackage/resilience`: resilience umbrella
- `github.com/platformcore/libpackage/resilience/retry`: retry
- `github.com/platformcore/libpackage/resilience/sanitizer`: sanitize/cleanup
- `github.com/platformcore/libpackage/resilience/validate`: validation utilities
- `github.com/platformcore/libpackage/resilience/validator`: validator helpers
- `github.com/platformcore/libpackage/resilience/pagination`: pagination helpers
- `github.com/platformcore/libpackage/resilience/cache`: cache resilience helpers
- `github.com/platformcore/libpackage/resilience/circuit`: circuit breaker
- `github.com/platformcore/libpackage/ratelimit`: rate limit umbrella
- `github.com/platformcore/libpackage/ratelimit/memory_store`: memory store for rate limit
- `github.com/platformcore/libpackage/ratelimit/enterprise`: enterprise rate-limit features

## 9) Middleware
- `github.com/platformcore/libpackage/middleware`: middleware umbrella
- `github.com/platformcore/libpackage/middleware/requestid`: request id middleware
- `github.com/platformcore/libpackage/middleware/timeout`: timeout middleware
- `github.com/platformcore/libpackage/middleware/logmid/logging-middleware`: logging middleware variant
- `github.com/platformcore/libpackage/middleware/adminshield/admin-middleware`: admin shield middleware variant
- `github.com/platformcore/libpackage/observability/logging-middleware`: standalone logging middleware
- `github.com/diftappdev/libpackage/adminshield`: standalone adminshield (namespace à¸žà¸´à¸¡à¸žà¹Œà¸•à¹ˆà¸²à¸‡à¸ˆà¸²à¸ driftappdev)
- `github.com/diftappdev/libpackage/auth`: standalone auth (namespace à¸žà¸´à¸¡à¸žà¹Œà¸•à¹ˆà¸²à¸‡à¸ˆà¸²à¸ driftappdev)

## 10) Legacy / Compatibility Facades (`go*`)
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

## 11) Folder Alias Map (à¸Šà¸·à¹ˆà¸­à¹‚à¸Ÿà¸¥à¹€à¸”à¸­à¸£à¹Œ != module path)
à¸à¸¥à¸¸à¹ˆà¸¡à¸™à¸µà¹‰à¹„à¸¡à¹ˆà¹ƒà¸Šà¹ˆà¹‚à¸¡à¸”à¸¹à¸¥à¹€à¸žà¸´à¹ˆà¸¡ à¹à¸•à¹ˆà¹€à¸›à¹‡à¸™à¹‚à¸Ÿà¸¥à¹€à¸”à¸­à¸£à¹Œà¸—à¸µà¹ˆà¸Šà¸µà¹‰à¹„à¸›à¸¢à¸±à¸‡ module path à¹€à¸Šà¸´à¸‡à¹‚à¸”à¹€à¸¡à¸™:
- `audit` -> `github.com/platformcore/libpackage/observability/audit`
- `auditv.1` -> `github.com/platformcore/libpackage/messaging/audit`
- `cache` -> `github.com/platformcore/libpackage/resilience/cache`
- `circuit` -> `github.com/platformcore/libpackage/resilience/circuit`
- `retry` -> `github.com/platformcore/libpackage/resilience/retry`
- `sanitizer` -> `github.com/platformcore/libpackage/resilience/sanitizer`
- `pagination` -> `github.com/platformcore/libpackage/resilience/pagination`
- `dlq` -> `github.com/platformcore/libpackage/messaging/dlq`
- `inbox` -> `github.com/platformcore/libpackage/messaging/inbox`
- `outbox` -> `github.com/platformcore/libpackage/messaging/outbox`
- `redrive` -> `github.com/platformcore/libpackage/messaging/redrive`
- `replay` -> `github.com/platformcore/libpackage/messaging/replay`
- `jwt` -> `github.com/platformcore/libpackage/security/jwt`
- `oauth2` -> `github.com/platformcore/libpackage/security/oauth2`
- `hash` -> `github.com/platformcore/libpackage/security/hash`
- `encryption` -> `github.com/platformcore/libpackage/security/encryption`
- `permission` -> `github.com/platformcore/libpackage/security/permission`
- `policy` -> `github.com/platformcore/libpackage/security/policy`
- `threatdefense` -> `github.com/platformcore/libpackage/security/threatdefense`
- `distlock` -> `github.com/platformcore/libpackage/persistence/distlock`
- `tx` -> `github.com/platformcore/libpackage/persistence/tx`
- `uow` -> `github.com/platformcore/libpackage/persistence/uow`
- `servicemesh` -> `github.com/platformcore/libpackage/platform/servicemesh`
- `common` -> `github.com/platformcore/libpackage/plugins/common`
- `engine` -> `github.com/platformcore/libpackage/plugins/engine`
- `enterprise` -> `github.com/platformcore/libpackage/ratelimit/enterprise`
- `performance` -> `github.com/platformcore/libpackage/observability/performance`
- `sentinelprofiler` -> `github.com/platformcore/libpackage/observability/sentinelprofiler`
- `ratelimitX` -> `github.com/platformcore/libpackage/ratelimit`

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



