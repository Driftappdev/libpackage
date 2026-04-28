# Libpackage Package Roles (Detailed)

เอกสารนี้สรุปบทบาทของแพ็กเกจใน `libpackage` แบบใช้งานจริง ว่าแต่ละตัวเหมาะกับ service ประเภทใด

## A) Foundation / Core
- `core`: แกนกลางของ error/context/logger/types; เหมาะกับทุก service
- `contracts`: DTO และ response envelope มาตรฐาน; เหมาะกับ API gateway และ backend service
- `config`: โหลด config/env/default; เหมาะกับทุก service โดยเฉพาะ microservice
- `result`: ผลลัพธ์แบบ typed result; เหมาะกับ service ที่ต้องการ error handling ชัด
- `runtime`: lifecycle, shutdown, health runtime; เหมาะกับ worker และ API ที่รันยาว
- `validator`: facade validation สำหรับ input/schema; เหมาะกับ API และ event consumer
- `client`: abstraction สำหรับ HTTP/gRPC/NATS client; เหมาะกับ integration service

## B) Dependency Injection / Composition
- `di`: orchestration DI ทั้งระบบ; เหมาะกับ monolith ที่แยก module และ microservice ขนาดกลาง-ใหญ่
- `registry`: service/component registry; เหมาะกับ plugin-heavy service และ runtime wiring
- `engine`: orchestration/engine runtime; เหมาะกับ workflow engine และ rules engine
- `workflow`: orchestration ของ step และ state; เหมาะกับ order, fulfillment, saga

## C) Messaging / Eventing
- `event`: middleware/instrumentation เชิง event; เหมาะกับ event pipeline
- `eventbus`: publish/subscribe abstraction; เหมาะกับ event-driven service
- `inbox`: ฝั่ง consume พร้อมควบคุม duplicate; เหมาะกับ consumer service
- `outbox`: transaction-safe producer dispatch; เหมาะกับ service ที่เขียน DB + publish event
- `dlq`: dead-letter queue management; เหมาะกับ consumer reliability
- `redrive`: ย้าย message กลับจาก DLQ; เหมาะกับ operations/maintenance job
- `replay`: replay message ตามช่วงเวลา/offset; เหมาะกับ incident recovery
- `idempotency`: ป้องกันซ้ำตอนรับ event/command; เหมาะกับ payment/order
- `example_integration`: ตัวอย่าง integration; เหมาะกับทีมที่ต้อง bootstrap เร็ว

## D) Security / Access Control
- `security`: security suite (token/api-key/signing/rate-limit/IP/crypto + HTTP headers)
- `jwt`: JWT specific flow
- `oauth2`: OAuth2 flow
- `hash`: hashing utility
- `encryption`: encryption utility
- `permission`: RBAC/permission check
- `policy`: policy evaluation
- `threatdefense`: suspicious behavior/threat hook
- `goauth`: legacy compatibility auth

เหมาะกับ auth service, API gateway, BFF, และ service ที่ต้องทำ machine-to-machine security

## E) Persistence / Consistency
- `persistence`: umbrella ด้าน persistence
- `tx`: transaction helper
- `uow`: unit-of-work
- `distlock`: distributed lock

เหมาะกับ service ที่ต้อง strict consistency เช่น payment, inventory, booking

## F) Observability / Telemetry
- `observability`: umbrella observability
- `correlation`: correlation/request ID propagation
- `tracing`: distributed tracing middleware/helper
- `trace`: trace context helper
- `span`: span helper utilities
- `metrics`: in-process metrics + HTTP middleware
- `profiler`: profiling/pprof helper
- `healthcheck`: liveness/readiness checks
- `telemetry`: OpenTelemetry-centric helper
- `performance`: performance-focused instrumentation
- `sentinelprofiler`: profiler integration variant
- `logging`: structured logging helper
- `logging-middleware`: request logging middleware
- `gometrics`, `gotracing`: legacy compatibility metrics/tracing

เหมาะกับทุก service โดยเฉพาะ production-grade ที่ต้อง SLA/SLO

## G) Resilience / Traffic Control
- `resilience`: umbrella resilience
- `ratelimitX`: canonical rate-limit package
- `goratelimit`: legacy/compat rate-limit package
- `goretry`: retry compatibility package
- `gocircuit`: circuit breaker compatibility package
- `gotimeout`: timeout compatibility package
- `gosanitizer`: sanitization compatibility package

เหมาะกับ external API-facing service, payment gateway, high-traffic API

## H) Platform / Plugin / Infra
- `platform`: platform integration layer
- `plugins`: plugin architecture
- `servicemesh`: service mesh helper
- `common`: shared utility ที่ใช้ข้าม package
- `policy`: policy-as-code runtime usage

เหมาะกับ enterprise platform team และ multi-tenant platform

## I) Standalone Concurrency / Control Modules (แยกโมดูลรายไฟล์)
- `adaptive_concurrency`: adaptive concurrency limiter
- `adaptive_retry`: adaptive retry strategy
- `backpressure`: backpressure strategy
- `batcher`: batching utility
- `bulkhead`: bulkhead isolation
- `cache_stampede_protection`: anti-stampede controls
- `checkpoint`: checkpoint utility
- `congestion_control`: congestion control utility
- `deadline`: deadline budget utility
- `distributed_rate_limit`: distributed rate-limit strategy
- `health_supervisor`: health supervision policy
- `hedged_request`: hedge request strategy
- `limiter`: generic limiter
- `load_shedder`: overload shedding
- `priority_queue`: prioritized queue
- `quorum`: quorum evaluation utility
- `request_coalescing`: request collapsing/coalescing
- `retry_backoff`: retry backoff policy
- `shadow_traffic`: shadow traffic routing
- `state_synchronizer`: state sync utility
- `token_bucket`: token bucket implementation
- `traffic_mirroring`: traffic mirror utility
- `workqueue`: worker queue utility

เหมาะกับ latency-sensitive service, gateway, queue worker, streaming processor

## J) Legacy / Compatibility Facade (`go*`)
- `goerror`, `gologger`, `gometrics`, `gotracing`, `goauth`, `goretry`, `gocircuit`, `gotimeout`, `gosanitizer`, `goratelimit`

เหมาะกับระบบที่ยัง migrate ไม่เสร็จ และต้องการ backward compatibility

---

## Suggested Canonical Use (ลดความซ้ำ)
- **Security**: ใช้ `security` เป็นหลัก แล้วค่อยใช้ `jwt/oauth2/hash/...` เฉพาะกรณี
- **Observability**: ใช้ `metrics/tracing/logging/healthcheck/correlation` เป็นหลัก
- **Resilience**: ใช้ `ratelimitX` + `resilience` เป็นหลัก, เก็บ `go*` เพื่อ compatibility
- **Messaging**: ใช้ `inbox/outbox/dlq/redrive/replay/idempotency` เป็นแกน

