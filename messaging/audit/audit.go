// Package audit provides enterprise-grade audit logging for distributed systems.
//
// Features:
//   - Structured, tamper-evident audit log entries
//   - Pluggable sinks (PostgreSQL, Elasticsearch, File, Multi-sink fan-out)
//   - Cryptographic entry chaining (SHA-256 hash chain)
//   - Async buffered writing with backpressure
//   - Sensitive field masking & PII redaction
//   - Correlation ID propagation
//   - Context-aware automatic field extraction
//   - GDPR/PCI-DSS compliant data retention tagging
//   - Replay-safe sequential event ordering
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────────
// Context Keys
// ────────────────────────────────────────────────────────────────────────────────

type contextKey string

const (
	ContextKeyUserID        contextKey = "audit_user_id"
	ContextKeyRequestID     contextKey = "audit_request_id"
	ContextKeyCorrelationID contextKey = "audit_correlation_id"
	ContextKeySessionID     contextKey = "audit_session_id"
	ContextKeyTenantID      contextKey = "audit_tenant_id"
	ContextKeyServiceName   contextKey = "audit_service_name"
	ContextKeyIPAddress     contextKey = "audit_ip_address"
	ContextKeyUserAgent     contextKey = "audit_user_agent"
)

// WithAuditContext enriches a context with audit metadata.
func WithAuditContext(ctx context.Context, meta AuditMeta) context.Context {
	ctx = context.WithValue(ctx, ContextKeyUserID, meta.UserID)
	ctx = context.WithValue(ctx, ContextKeyRequestID, meta.RequestID)
	ctx = context.WithValue(ctx, ContextKeyCorrelationID, meta.CorrelationID)
	ctx = context.WithValue(ctx, ContextKeySessionID, meta.SessionID)
	ctx = context.WithValue(ctx, ContextKeyTenantID, meta.TenantID)
	ctx = context.WithValue(ctx, ContextKeyServiceName, meta.ServiceName)
	ctx = context.WithValue(ctx, ContextKeyIPAddress, meta.IPAddress)
	ctx = context.WithValue(ctx, ContextKeyUserAgent, meta.UserAgent)
	return ctx
}

// AuditMeta holds tracing metadata extracted from context.
type AuditMeta struct {
	UserID        string
	RequestID     string
	CorrelationID string
	SessionID     string
	TenantID      string
	ServiceName   string
	IPAddress     string
	UserAgent     string
}

func auditMetaFromContext(ctx context.Context) AuditMeta {
	str := func(k contextKey) string {
		v, _ := ctx.Value(k).(string)
		return v
	}
	return AuditMeta{
		UserID:        str(ContextKeyUserID),
		RequestID:     str(ContextKeyRequestID),
		CorrelationID: str(ContextKeyCorrelationID),
		SessionID:     str(ContextKeySessionID),
		TenantID:      str(ContextKeyTenantID),
		ServiceName:   str(ContextKeyServiceName),
		IPAddress:     str(ContextKeyIPAddress),
		UserAgent:     str(ContextKeyUserAgent),
	}
}

// ────────────────────────────────────────────────────────────────────────────────
// Event Categories & Actions
// ────────────────────────────────────────────────────────────────────────────────

type Category string
type Action string
type Severity string
type Outcome string

const (
	// Categories
	CategoryAuth       Category = "AUTH"
	CategoryFinance    Category = "FINANCE"
	CategoryPoints     Category = "POINTS"
	CategoryCoupon     Category = "COUPON"
	CategoryUser       Category = "USER"
	CategoryAdmin      Category = "ADMIN"
	CategorySystem     Category = "SYSTEM"
	CategorySecurity   Category = "SECURITY"
	CategoryData       Category = "DATA"
	CategoryPermission Category = "PERMISSION"

	// Actions
	ActionCreate   Action = "CREATE"
	ActionRead     Action = "READ"
	ActionUpdate   Action = "UPDATE"
	ActionDelete   Action = "DELETE"
	ActionLogin    Action = "LOGIN"
	ActionLogout   Action = "LOGOUT"
	ActionApprove  Action = "APPROVE"
	ActionReject   Action = "REJECT"
	ActionTransfer Action = "TRANSFER"
	ActionDebit    Action = "DEBIT"
	ActionCredit   Action = "CREDIT"
	ActionRefund   Action = "REFUND"
	ActionRedeem   Action = "REDEEM"
	ActionExpire   Action = "EXPIRE"
	ActionRevoke   Action = "REVOKE"
	ActionGrant    Action = "GRANT"
	ActionExport   Action = "EXPORT"
	ActionImport   Action = "IMPORT"
	ActionSearch   Action = "SEARCH"

	// Severities
	SeverityDebug    Severity = "DEBUG"
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityCritical Severity = "CRITICAL"
	SeverityAlert    Severity = "ALERT"

	// Outcomes
	OutcomeSuccess Outcome = "SUCCESS"
	OutcomeFailure Outcome = "FAILURE"
	OutcomeDenied  Outcome = "DENIED"
	OutcomePartial Outcome = "PARTIAL"
)

// ────────────────────────────────────────────────────────────────────────────────
// Entry — the core audit record
// ────────────────────────────────────────────────────────────────────────────────

// Entry is a single immutable audit log record.
type Entry struct {
	// Identity
	ID           string `json:"id"`
	SequenceNo   uint64 `json:"sequence_no"`
	PreviousHash string `json:"previous_hash"` // Hash-chaining for tamper detection
	Hash         string `json:"hash"`

	// Classification
	Category Category `json:"category"`
	Action   Action   `json:"action"`
	Severity Severity `json:"severity"`
	Outcome  Outcome  `json:"outcome"`

	// Principals
	UserID        string `json:"user_id,omitempty"`
	TenantID      string `json:"tenant_id,omitempty"`
	ActorType     string `json:"actor_type,omitempty"` // "user", "service", "system"
	ServiceName   string `json:"service_name"`
	OperationName string `json:"operation_name,omitempty"`

	// Target resource
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`

	// Request tracing
	RequestID      string `json:"request_id,omitempty"`
	CorrelationID  string `json:"correlation_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`

	// Network
	IPAddress string `json:"ip_address,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`

	// Timing
	Timestamp time.Time      `json:"timestamp"`
	Duration  *time.Duration `json:"duration_ms,omitempty"`

	// Payload (sanitized)
	Before json.RawMessage `json:"before,omitempty"` // State before mutation
	After  json.RawMessage `json:"after,omitempty"`  // State after mutation
	Delta  json.RawMessage `json:"delta,omitempty"`  // Changed fields
	Error  string          `json:"error,omitempty"`

	// Compliance
	DataClassification string            `json:"data_classification,omitempty"` // PII, PCI, PHI, PUBLIC
	RetentionDays      int               `json:"retention_days,omitempty"`
	Tags               []string          `json:"tags,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`

	// Source code location (debug)
	Caller string `json:"caller,omitempty"`
}

// computeHash generates the SHA-256 hash of this entry for chain integrity.
func (e *Entry) computeHash() string {
	h := sha256.New()
	fields := []string{
		e.ID, e.PreviousHash,
		string(e.Category), string(e.Action), string(e.Outcome),
		e.UserID, e.ServiceName, e.ResourceType, e.ResourceID,
		e.RequestID, e.Timestamp.UTC().Format(time.RFC3339Nano),
	}
	h.Write([]byte(strings.Join(fields, "|")))
	return hex.EncodeToString(h.Sum(nil))
}

// ────────────────────────────────────────────────────────────────────────────────
// Sink Interface
// ────────────────────────────────────────────────────────────────────────────────

// Sink is the pluggable destination for audit entries.
type Sink interface {
	Write(ctx context.Context, entries []*Entry) error
	Flush(ctx context.Context) error
	Close() error
	Name() string
}

// ────────────────────────────────────────────────────────────────────────────────
// Console / Stdout Sink
// ────────────────────────────────────────────────────────────────────────────────

// ConsoleSink writes JSON-encoded entries to stdout.
type ConsoleSink struct{ name string }

func NewConsoleSink() *ConsoleSink                   { return &ConsoleSink{name: "console"} }
func (s *ConsoleSink) Name() string                  { return s.name }
func (s *ConsoleSink) Close() error                  { return nil }
func (s *ConsoleSink) Flush(_ context.Context) error { return nil }
func (s *ConsoleSink) Write(_ context.Context, entries []*Entry) error {
	enc := json.NewEncoder(os.Stdout)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────────
// File Sink (append-only JSONL)
// ────────────────────────────────────────────────────────────────────────────────

// FileSink writes JSONL audit entries to a file.
type FileSink struct {
	mu   sync.Mutex
	file *os.File
	name string
}

func NewFileSink(path string) (*FileSink, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: failed to open file sink: %w", err)
	}
	return &FileSink{file: f, name: "file:" + path}, nil
}

func (s *FileSink) Name() string { return s.name }
func (s *FileSink) Close() error { return s.file.Close() }
func (s *FileSink) Flush(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Sync()
}
func (s *FileSink) Write(_ context.Context, entries []*Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	enc := json.NewEncoder(s.file)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────────
// Multi-Sink fan-out
// ────────────────────────────────────────────────────────────────────────────────

// MultiSink writes to multiple sinks concurrently.
type MultiSink struct {
	sinks []Sink
}

func NewMultiSink(sinks ...Sink) *MultiSink { return &MultiSink{sinks: sinks} }
func (m *MultiSink) Name() string           { return "multi" }
func (m *MultiSink) Close() error {
	var errs []string
	for _, s := range m.sinks {
		if err := s.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
func (m *MultiSink) Flush(ctx context.Context) error {
	for _, s := range m.sinks {
		_ = s.Flush(ctx)
	}
	return nil
}
func (m *MultiSink) Write(ctx context.Context, entries []*Entry) error {
	var wg sync.WaitGroup
	errs := make([]error, len(m.sinks))
	for i, s := range m.sinks {
		wg.Add(1)
		go func(i int, s Sink) {
			defer wg.Done()
			errs[i] = s.Write(ctx, entries)
		}(i, s)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────────
// PII Masker
// ────────────────────────────────────────────────────────────────────────────────

// MaskFunc is called on field values matching a sensitive key pattern.
type MaskFunc func(key, value string) string

// DefaultMaskFunc replaces the middle of a string with asterisks.
func DefaultMaskFunc(_, value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}

// ────────────────────────────────────────────────────────────────────────────────
// Logger — the main API
// ────────────────────────────────────────────────────────────────────────────────

// LoggerConfig configures the audit logger.
type LoggerConfig struct {
	ServiceName          string
	BufferSize           int           // Async buffer size (default: 4096)
	FlushInterval        time.Duration // Background flush interval (default: 5s)
	SensitiveFields      []string      // Field keys to mask
	MaskFunc             MaskFunc
	DefaultRetentionDays int
	EnableHashChain      bool
	EnableCaller         bool
	ErrorHandler         func(err error) // Called on sink write errors
}

func (c *LoggerConfig) defaults() {
	if c.BufferSize == 0 {
		c.BufferSize = 4096
	}
	if c.FlushInterval == 0 {
		c.FlushInterval = 5 * time.Second
	}
	if c.DefaultRetentionDays == 0 {
		c.DefaultRetentionDays = 2555 // 7 years
	}
	if c.MaskFunc == nil {
		c.MaskFunc = DefaultMaskFunc
	}
	if c.ErrorHandler == nil {
		c.ErrorHandler = func(err error) {
			fmt.Fprintf(os.Stderr, "[AUDIT ERROR] %v\n", err)
		}
	}
}

// Logger is the enterprise audit logger.
type Logger struct {
	sink       Sink
	config     LoggerConfig
	buf        chan *Entry
	done       chan struct{}
	wg         sync.WaitGroup
	seq        atomic.Uint64
	lastHashMu sync.Mutex
	lastHash   string
	sensitive  map[string]struct{}
}

// NewLogger creates and starts a new audit Logger.
func NewLogger(sink Sink, cfg LoggerConfig) *Logger {
	cfg.defaults()
	sensitive := make(map[string]struct{})
	for _, f := range cfg.SensitiveFields {
		sensitive[strings.ToLower(f)] = struct{}{}
	}
	l := &Logger{
		sink:      sink,
		config:    cfg,
		buf:       make(chan *Entry, cfg.BufferSize),
		done:      make(chan struct{}),
		sensitive: sensitive,
	}
	l.wg.Add(1)
	go l.drainLoop()
	return l
}

// Log records an audit event. This is the primary method.
func (l *Logger) Log(ctx context.Context, e *Entry) {
	meta := auditMetaFromContext(ctx)

	// Fill in from context if not set.
	if e.UserID == "" {
		e.UserID = meta.UserID
	}
	if e.TenantID == "" {
		e.TenantID = meta.TenantID
	}
	if e.ServiceName == "" {
		e.ServiceName = coalesce(meta.ServiceName, l.config.ServiceName)
	}
	if e.RequestID == "" {
		e.RequestID = meta.RequestID
	}
	if e.CorrelationID == "" {
		e.CorrelationID = meta.CorrelationID
	}
	if e.SessionID == "" {
		e.SessionID = meta.SessionID
	}
	if e.IPAddress == "" {
		e.IPAddress = meta.IPAddress
	}
	if e.UserAgent == "" {
		e.UserAgent = meta.UserAgent
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if e.Severity == "" {
		e.Severity = SeverityInfo
	}
	if e.RetentionDays == 0 {
		e.RetentionDays = l.config.DefaultRetentionDays
	}

	// Sequence & hash chain.
	e.SequenceNo = l.seq.Add(1)
	if l.config.EnableHashChain {
		l.lastHashMu.Lock()
		e.PreviousHash = l.lastHash
		e.Hash = e.computeHash()
		l.lastHash = e.Hash
		l.lastHashMu.Unlock()
	}

	// Caller info.
	if l.config.EnableCaller && e.Caller == "" {
		_, file, line, ok := runtime.Caller(2)
		if ok {
			e.Caller = fmt.Sprintf("%s:%d", trimPath(file), line)
		}
	}

	// Sanitize sensitive data.
	l.sanitize(e)

	select {
	case l.buf <- e:
	default:
		// Buffer full — write synchronously.
		_ = l.sink.Write(ctx, []*Entry{e})
	}
}

// LogEvent is a convenience builder for quick log calls.
func (l *Logger) LogEvent(ctx context.Context, opts ...EntryOption) {
	e := &Entry{}
	for _, o := range opts {
		o(e)
	}
	l.Log(ctx, e)
}

// Flush forces all buffered entries to be written.
func (l *Logger) Flush(ctx context.Context) error {
	return l.sink.Flush(ctx)
}

// Shutdown gracefully drains the buffer and closes the sink.
func (l *Logger) Shutdown(ctx context.Context) error {
	close(l.done)
	l.wg.Wait()
	return l.sink.Close()
}

func (l *Logger) drainLoop() {
	defer l.wg.Done()
	ticker := time.NewTicker(l.config.FlushInterval)
	defer ticker.Stop()

	batch := make([]*Entry, 0, 256)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := l.sink.Write(context.Background(), batch); err != nil {
			l.config.ErrorHandler(err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case e := <-l.buf:
			batch = append(batch, e)
			if len(batch) >= 256 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-l.done:
			// Drain remaining.
			for {
				select {
				case e := <-l.buf:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		}
	}
}

func (l *Logger) sanitize(e *Entry) {
	if len(l.sensitive) == 0 {
		return
	}
	e.Before = maskJSON(e.Before, l.sensitive, l.config.MaskFunc)
	e.After = maskJSON(e.After, l.sensitive, l.config.MaskFunc)
	e.Delta = maskJSON(e.Delta, l.sensitive, l.config.MaskFunc)
}

// ────────────────────────────────────────────────────────────────────────────────
// EntryOption (builder pattern)
// ────────────────────────────────────────────────────────────────────────────────

// EntryOption is a functional option for building Entry objects.
type EntryOption func(*Entry)

func WithID(id string) EntryOption         { return func(e *Entry) { e.ID = id } }
func WithCategory(c Category) EntryOption  { return func(e *Entry) { e.Category = c } }
func WithAction(a Action) EntryOption      { return func(e *Entry) { e.Action = a } }
func WithSeverity(s Severity) EntryOption  { return func(e *Entry) { e.Severity = s } }
func WithOutcome(o Outcome) EntryOption    { return func(e *Entry) { e.Outcome = o } }
func WithUserID(uid string) EntryOption    { return func(e *Entry) { e.UserID = uid } }
func WithTenantID(tid string) EntryOption  { return func(e *Entry) { e.TenantID = tid } }
func WithServiceName(s string) EntryOption { return func(e *Entry) { e.ServiceName = s } }
func WithOperation(op string) EntryOption  { return func(e *Entry) { e.OperationName = op } }
func WithResource(rtype, rid string) EntryOption {
	return func(e *Entry) { e.ResourceType = rtype; e.ResourceID = rid }
}
func WithRequestID(rid string) EntryOption     { return func(e *Entry) { e.RequestID = rid } }
func WithCorrelationID(cid string) EntryOption { return func(e *Entry) { e.CorrelationID = cid } }
func WithIPAddress(ip string) EntryOption      { return func(e *Entry) { e.IPAddress = ip } }
func WithDuration(d time.Duration) EntryOption { return func(e *Entry) { e.Duration = &d } }
func WithError(err error) EntryOption {
	return func(e *Entry) {
		if err != nil {
			e.Error = err.Error()
			e.Outcome = OutcomeFailure
		}
	}
}
func WithBefore(v any) EntryOption {
	return func(e *Entry) { e.Before, _ = json.Marshal(v) }
}
func WithAfter(v any) EntryOption {
	return func(e *Entry) { e.After, _ = json.Marshal(v) }
}
func WithDelta(v any) EntryOption {
	return func(e *Entry) { e.Delta, _ = json.Marshal(v) }
}
func WithTags(tags ...string) EntryOption {
	return func(e *Entry) { e.Tags = append(e.Tags, tags...) }
}
func WithMetadata(k, v string) EntryOption {
	return func(e *Entry) {
		if e.Metadata == nil {
			e.Metadata = make(map[string]string)
		}
		e.Metadata[k] = v
	}
}
func WithDataClassification(dc string) EntryOption {
	return func(e *Entry) { e.DataClassification = dc }
}
func WithIdempotencyKey(key string) EntryOption {
	return func(e *Entry) { e.IdempotencyKey = key }
}
func WithActorType(at string) EntryOption { return func(e *Entry) { e.ActorType = at } }

// ────────────────────────────────────────────────────────────────────────────────
// Financial / Domain Helpers
// ────────────────────────────────────────────────────────────────────────────────

// FinancialEntry creates a pre-populated entry for financial transactions.
func FinancialEntry(action Action, resourceType, resourceID string, before, after any) []EntryOption {
	return []EntryOption{
		WithCategory(CategoryFinance),
		WithAction(action),
		WithResource(resourceType, resourceID),
		WithBefore(before),
		WithAfter(after),
		WithDataClassification("PCI"),
		WithSeverity(SeverityInfo),
		WithOutcome(OutcomeSuccess),
	}
}

// PointsEntry creates a pre-populated entry for points operations.
func PointsEntry(action Action, userID, resourceID string, before, after any) []EntryOption {
	return []EntryOption{
		WithCategory(CategoryPoints),
		WithAction(action),
		WithUserID(userID),
		WithResource("points_account", resourceID),
		WithBefore(before),
		WithAfter(after),
		WithSeverity(SeverityInfo),
		WithOutcome(OutcomeSuccess),
	}
}

// CouponEntry creates a pre-populated entry for coupon operations.
func CouponEntry(action Action, userID, couponID string) []EntryOption {
	return []EntryOption{
		WithCategory(CategoryCoupon),
		WithAction(action),
		WithUserID(userID),
		WithResource("coupon", couponID),
		WithSeverity(SeverityInfo),
		WithOutcome(OutcomeSuccess),
	}
}

// SecurityEntry creates a high-severity security audit entry.
func SecurityEntry(action Action, outcome Outcome, description string) []EntryOption {
	return []EntryOption{
		WithCategory(CategorySecurity),
		WithAction(action),
		WithOutcome(outcome),
		WithSeverity(SeverityCritical),
		WithMetadata("description", description),
	}
}

// ────────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ────────────────────────────────────────────────────────────────────────────────

func maskJSON(data json.RawMessage, sensitive map[string]struct{}, maskFn MaskFunc) json.RawMessage {
	if len(data) == 0 {
		return data
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return data // Not a JSON object, return as-is.
	}
	maskMap(m, sensitive, maskFn)
	out, _ := json.Marshal(m)
	return out
}

func maskMap(m map[string]any, sensitive map[string]struct{}, maskFn MaskFunc) {
	for k, v := range m {
		lk := strings.ToLower(k)
		if _, ok := sensitive[lk]; ok {
			if str, ok := v.(string); ok {
				m[k] = maskFn(k, str)
			} else {
				m[k] = "****"
			}
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			maskMap(nested, sensitive, maskFn)
		}
	}
}

func trimPath(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx == -1 {
		return path
	}
	return path[idx+1:]
}

func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ────────────────────────────────────────────────────────────────────────────────
// Verifier — tamper detection
// ────────────────────────────────────────────────────────────────────────────────

// VerifyChain verifies the hash chain integrity of a sequence of entries.
// Returns the index of the first broken link, or -1 if chain is intact.
func VerifyChain(entries []*Entry) (brokenIndex int, err error) {
	prevHash := ""
	for i, e := range entries {
		if e.PreviousHash != prevHash {
			return i, fmt.Errorf("audit: hash chain broken at entry %d (seq=%d)", i, e.SequenceNo)
		}
		expected := e.computeHash()
		if e.Hash != expected {
			return i, fmt.Errorf("audit: entry %d (seq=%d) hash mismatch: expected %s got %s",
				i, e.SequenceNo, expected, e.Hash)
		}
		prevHash = e.Hash
	}
	return -1, nil
}
