# Libpackage Capability Gap Analysis

วิเคราะห์ช่องว่างความสามารถจากระดับพื้นฐานถึงระดับองค์กร หลังการย้าย/ยุบโมดูลล่าสุด

## 1) พื้นฐาน (Foundation) ที่ยังควรเติม
- ชุด `testing/testkit` กลางสำหรับ mock fixtures, contract assertions, integration harness
- ชุด `errors/catalog` กลางที่ map domain error -> transport error (HTTP/gRPC/event)
- ชุด `serialization` กลางที่รองรับ versioned schema + backward compatibility policy

## 2) ระดับบริการ (Service Runtime) ที่ควรเสริม
- `service-discovery` abstraction ชัดเจน (ปัจจุบันเอนที่ `servicemesh/platform`)
- `config hot-reload` + dynamic config snapshot/rollback
- `feature rollout guardrail` เชื่อม feature flag + SLO auto rollback

## 3) ข้อมูลและ persistence ระดับองค์กร
- migration orchestration package กลาง (plan/apply/verify/rollback)
- data access policy layer (tenant boundary guard, PII access policy)
- read/write split + replica routing strategy ที่ reusable

## 4) Messaging/Streaming ระดับองค์กร
- schema registry integration (avro/proto/json-schema) พร้อม compatibility check
- exactly-once helper ระดับ workflow (inbox/outbox มีแล้วแต่ยังไม่มี orchestration layer กลาง)
- event version migration helpers (upcast/downcast policy)

## 5) Security / Compliance ที่ยังขาดเชิงองค์กร
- secrets rotation orchestrator (policy + automation)
- audit trail package ที่ map compliance standard (SOC2/PCI/GDPR) เป็น control checklist
- DLP helper สำหรับ log redaction และ data egress policy

## 6) Observability / SRE ที่ยังขาด
- unified SLO package (SLO definition + burn-rate alerts helpers)
- incident context package (timeline/correlation snapshot สำหรับ postmortem)
- trace/metric/log correlation exporter กลางแบบ opinionated

## 7) Platform / Plugin Governance
- plugin sandbox policy (capability boundary, permission manifest)
- plugin lifecycle policy (compat matrix, deprecation window, upgrade contract)
- extension API version negotiation package

## 8) Developer Experience / Operability
- codegen package สำหรับ module scaffold มาตรฐาน
- dependency policy checker (ห้าม import ข้าม bounded context ผิดชั้น)
- package scorecard (owner, maturity, SLA, support tier)

---

## Priority Roadmap (แนะนำลำดับทำ)
1. `testing/testkit` + `errors/catalog`
2. `schema-registry` + `event-version-migration`
3. `slo` + `incident-context`
4. `secrets-rotation` + `dlp`
5. `plugin-governance` + `dependency-policy-checker`

## Current State Summary
- จุดแข็ง: โมดูลด้าน resilience/observability/security มี breadth ครบ
- จุดต้องเร่ง: governance, compliance automation, และ standardized testability
- หลังรีแฟกเตอร์: ความซ้ำด้าน middleware ลดลง และโมดูลใช้งานระดับ root ชัดขึ้น

