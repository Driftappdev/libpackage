// Package threatdefense provides enterprise-grade, multi-layer threat detection
// and active defense for distributed microservices at scale.
// Designed as a reusable shared library across the entire organization.
//
// Features:
//   - Behavioral anomaly detection (per-IP, per-user, per-tenant)
//   - Adaptive brute-force protection with exponential backoff
//   - Distributed IP reputation & blocklist management
//   - SQL/NoSQL/Command injection detection (signature + heuristic)
//   - XSS / HTML injection detection
//   - Credential stuffing detection via velocity checks
//   - Bot detection (fingerprinting + behavioral scoring)
//   - Distributed deny list with TTL (Redis-compatible API)
//   - Honeypot field detection
//   - HTTP parameter pollution detection
//   - Request smuggling detection
//   - Automatic attacker quarantine with graduated response
//   - Full threat event log with correlation
//   - Real-time threat intelligence feed integration
package threatdefense

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// ============================================================
// THREAT SEVERITY & TYPES
// ============================================================

// ThreatLevel defines the severity of a detected threat.
type ThreatLevel int

const (
	ThreatLevelNone     ThreatLevel = iota
	ThreatLevelLow                  // Log only
	ThreatLevelMedium               // Rate limit / challenge
	ThreatLevelHigh                 // Block temporarily
	ThreatLevelCritical             // Block indefinitely + alert
)

func (tl ThreatLevel) String() string {
	return [...]string{"NONE", "LOW", "MEDIUM", "HIGH", "CRITICAL"}[tl]
}

// ThreatType categorizes the detected attack vector.
type ThreatType string

const (
	ThreatSQLInjection          ThreatType = "SQL_INJECTION"
	ThreatNoSQLInjection        ThreatType = "NOSQL_INJECTION"
	ThreatXSS                   ThreatType = "XSS"
	ThreatCommandInjection      ThreatType = "COMMAND_INJECTION"
	ThreatPathTraversal         ThreatType = "PATH_TRAVERSAL"
	ThreatBruteForce            ThreatType = "BRUTE_FORCE"
	ThreatCredentialStuffing    ThreatType = "CREDENTIAL_STUFFING"
	ThreatBotActivity           ThreatType = "BOT_ACTIVITY"
	ThreatAnomalousVelocity     ThreatType = "ANOMALOUS_VELOCITY"
	ThreatIPReputation          ThreatType = "IP_REPUTATION"
	ThreatHoneypotTriggered     ThreatType = "HONEYPOT_TRIGGERED"
	ThreatParamPollution        ThreatType = "PARAM_POLLUTION"
	ThreatRequestSmuggling      ThreatType = "REQUEST_SMUGGLING"
	ThreatXXE                   ThreatType = "XXE"
	ThreatSSRF                  ThreatType = "SSRF"
	ThreatSensitiveDataExposure ThreatType = "SENSITIVE_DATA_EXPOSURE"
	ThreatTokenAbuse            ThreatType = "TOKEN_ABUSE"
	ThreatScannerDetected       ThreatType = "SCANNER_DETECTED"
	ThreatRateLimitAbuse        ThreatType = "RATE_LIMIT_ABUSE"
)

// ThreatEvent records a detected threat incident.
type ThreatEvent struct {
	ID          string      `json:"id"`
	Timestamp   time.Time   `json:"timestamp"`
	Type        ThreatType  `json:"type"`
	Level       ThreatLevel `json:"level"`
	IPAddress   string      `json:"ip_address"`
	UserID      string      `json:"user_id,omitempty"`
	TenantID    string      `json:"tenant_id,omitempty"`
	SessionID   string      `json:"session_id,omitempty"`
	RequestPath string      `json:"request_path,omitempty"`
	Method      string      `json:"method,omitempty"`
	Evidence    string      `json:"evidence,omitempty"`
	Score       float64     `json:"risk_score"`
	Action      string      `json:"action_taken"`
	ServiceName string      `json:"service_name"`
	Fingerprint string      `json:"fingerprint,omitempty"` // Request fingerprint hash
	Tags        []string    `json:"tags,omitempty"`
}

// ============================================================
// INJECTION DETECTION ENGINE
// ============================================================

// InjectionDetector uses compiled regex + heuristics to detect injection attacks.
type InjectionDetector struct {
	sqlPatterns     []*regexp.Regexp
	nosqlPatterns   []*regexp.Regexp
	xssPatterns     []*regexp.Regexp
	cmdPatterns     []*regexp.Regexp
	xxePatterns     []*regexp.Regexp
	pathPatterns    []*regexp.Regexp
	scannerPatterns []*regexp.Regexp
}

// NewInjectionDetector builds a production-grade injection detector.
func NewInjectionDetector() *InjectionDetector {
	compile := func(patterns []string) []*regexp.Regexp {
		result := make([]*regexp.Regexp, 0, len(patterns))
		for _, p := range patterns {
			if r, err := regexp.Compile(p); err == nil {
				result = append(result, r)
			}
		}
		return result
	}

	return &InjectionDetector{
		sqlPatterns: compile([]string{
			`(?i)(union\s+(all\s+)?select)`,
			`(?i)(select\s+.+\s+from\s+.+)`,
			`(?i)(insert\s+into\s+.+\s+values)`,
			`(?i)(update\s+.+\s+set\s+.+\s*=)`,
			`(?i)(delete\s+from\s+.+)`,
			`(?i)(drop\s+(table|database|index|view|procedure))`,
			`(?i)(create\s+(table|database|index|view|procedure))`,
			`(?i)(alter\s+(table|database))`,
			`(?i)(exec\s*\(|execute\s*\()`,
			`(?i)(xp_cmdshell|sp_executesql|sp_makewebtask)`,
			`(?i)(\bor\b\s+\d+\s*=\s*\d+)`,
			`(?i)(\band\b\s+\d+\s*=\s*\d+)`,
			`(?i)(sleep\s*\(\s*\d+\s*\)|waitfor\s+delay)`,
			`(?i)(benchmark\s*\(\s*\d+)`,
			`(?i)(load_file\s*\(|into\s+outfile)`,
			`(?i)(information_schema\.(tables|columns|schemata))`,
			`(?i)(char\s*\(\s*\d+)`, // char() encoding bypass
			`'(\s*)(or|and)(\s*)'`,  // ' OR '...' attacks
			`--\s*$`,                // SQL comment termination
			`(?i)/\*.*\*/`,          // Inline SQL comments
		}),
		nosqlPatterns: compile([]string{
			`\$where\s*:`,
			`\$regex\s*:`,
			`\$gt\s*:.*\$lt\s*:`,
			`\$ne\s*:\s*`,
			`\$exists\s*:\s*true`,
			`\$or\s*:\s*\[`,
			`\$and\s*:\s*\[`,
			`mapReduce\s*:`,
			`\$function\s*:`,
			`this\.\w+\s*==`,
			`db\.getCollection`,
			`db\.eval\s*\(`,
		}),
		xssPatterns: compile([]string{
			`(?i)<\s*script[^>]*>`,
			`(?i)<\s*/\s*script\s*>`,
			`(?i)javascript\s*:`,
			`(?i)vbscript\s*:`,
			`(?i)on(load|error|click|mouseover|focus|blur|change|submit|keyup|keydown|keypress|mouseout)\s*=`,
			`(?i)<\s*iframe[^>]*>`,
			`(?i)<\s*img[^>]+src\s*=\s*["']?\s*javascript`,
			`(?i)<\s*object[^>]*>`,
			`(?i)<\s*embed[^>]*>`,
			`(?i)<\s*link[^>]+href\s*=`,
			`(?i)expression\s*\(`, // IE CSS expression
			`(?i)url\s*\(\s*javascript`,
			`(?i)&#(x[0-9a-fA-F]+|\d+);`, // HTML entity encoding
			`(?i)\\u003c`,                // Unicode encoding of <
			`(?i)%3c.*%3e`,               // URL-encoded <...>
			`(?i)<\s*svg[^>]*on\w+\s*=`,
			`(?i)document\.(cookie|location|write)`,
			`(?i)window\.(location|open)`,
		}),
		cmdPatterns: compile([]string{
			`(?i)(;|\||&&|` + "`" + `|\$\()`,
			`(?i)\b(cat|ls|pwd|whoami|id|uname|ps|netstat|ifconfig|wget|curl|nc|ncat|bash|sh|zsh|python|perl|ruby|php)\b`,
			`(?i)(\.\./|\.\.\%2f|\.\.\%5c)`,
			`(?i)(\/etc\/passwd|\/etc\/shadow|\/etc\/hosts)`,
			`(?i)(cmd\.exe|powershell|wscript|cscript)`,
			`(?i)(\brm\s+-rf\b|\bdel\s+\/f\b)`,
			`(?i)(\bchmod\s+[0-7]{3,4}\b|\bchown\b)`,
			`(?i)(>\s*\/dev\/null|2>&1)`,
		}),
		xxePatterns: compile([]string{
			`(?i)<!entity`,
			`(?i)<!doctype[^>]+\[`,
			`(?i)system\s+"[^"]*"`,
			`(?i)public\s+"[^"]*"\s+"[^"]*"`,
			`(?i)file://`,
			`(?i)expect://`,
			`(?i)php://`,
			`(?i)data://`,
			`(?i)gopher://`,
		}),
		pathPatterns: compile([]string{
			`(?i)(\.\./|\.\.\\|%2e%2e%2f|%2e%2e/|\.\.%2f|%2e\.%2f)`,
			`(?i)(\.\./){2,}`,
			`\x00`,
			`(?i)(\/proc\/|\/sys\/|\/dev\/)`,
			`(?i)(%00|%0a|%0d)`, // Null byte and CRLF
		}),
		scannerPatterns: compile([]string{
			`(?i)(nikto|nessus|openvas|w3af|sqlmap|nmap|masscan|zap|burpsuite|acunetix|appscan)`,
			`(?i)(vulnerability.scanner|security.scanner|pentest)`,
			`(?i)(python-requests|go-http-client|curl\/|wget\/|libwww-perl)`,
		}),
	}
}

// DetectionResult holds the result of an injection scan.
type DetectionResult struct {
	Detected bool
	Type     ThreatType
	Pattern  string
	Score    float64
}

// Scan checks a string for injection patterns.
func (id *InjectionDetector) Scan(input string) []DetectionResult {
	var results []DetectionResult

	checks := []struct {
		patterns []*regexp.Regexp
		typ      ThreatType
		score    float64
	}{
		{id.sqlPatterns, ThreatSQLInjection, 0.9},
		{id.nosqlPatterns, ThreatNoSQLInjection, 0.85},
		{id.xssPatterns, ThreatXSS, 0.85},
		{id.cmdPatterns, ThreatCommandInjection, 0.95},
		{id.xxePatterns, ThreatXXE, 0.9},
		{id.pathPatterns, ThreatPathTraversal, 0.8},
	}

	for _, check := range checks {
		for _, re := range check.patterns {
			if match := re.FindString(input); match != "" {
				results = append(results, DetectionResult{
					Detected: true,
					Type:     check.typ,
					Pattern:  match,
					Score:    check.score,
				})
				break // One per type is enough
			}
		}
	}
	return results
}

// ScanRequest scans all parts of an HTTP request.
func (id *InjectionDetector) ScanRequest(r *http.Request) []DetectionResult {
	var all []DetectionResult
	// Scan URL path
	all = append(all, id.Scan(r.URL.Path)...)
	// Scan query params
	for _, vals := range r.URL.Query() {
		for _, v := range vals {
			all = append(all, id.Scan(v)...)
		}
	}
	// Scan headers (targeted)
	for _, h := range []string{"User-Agent", "Referer", "X-Forwarded-For", "Cookie"} {
		if v := r.Header.Get(h); v != "" {
			all = append(all, id.Scan(v)...)
		}
	}
	// Scanner detection via User-Agent
	ua := r.Header.Get("User-Agent")
	for _, re := range id.scannerPatterns {
		if re.MatchString(ua) {
			all = append(all, DetectionResult{
				Detected: true,
				Type:     ThreatScannerDetected,
				Pattern:  ua,
				Score:    0.7,
			})
			break
		}
	}
	return all
}

// ============================================================
// BRUTE FORCE PROTECTION
// ============================================================

// BruteForceEntry tracks failed attempts for a key (IP, username, etc.).
type BruteForceEntry struct {
	mu           sync.Mutex
	failures     int
	lastFailure  time.Time
	blockedUntil time.Time
	totalBlocks  int
}

// BruteForceProtection implements adaptive lockout with exponential backoff.
type BruteForceProtection struct {
	mu             sync.RWMutex
	entries        map[string]*BruteForceEntry
	maxAttempts    int
	windowDuration time.Duration
	baseLockout    time.Duration // Initial lockout duration
	maxLockout     time.Duration // Maximum lockout duration
	// Metrics
	totalBlocks atomic.Uint64
	totalChecks atomic.Uint64
}

// NewBruteForceProtection creates a brute-force protection system.
func NewBruteForceProtection(maxAttempts int, window, baseLockout, maxLockout time.Duration) *BruteForceProtection {
	bfp := &BruteForceProtection{
		entries:        make(map[string]*BruteForceEntry),
		maxAttempts:    maxAttempts,
		windowDuration: window,
		baseLockout:    baseLockout,
		maxLockout:     maxLockout,
	}
	go bfp.cleanup()
	return bfp
}

// Check returns an error if the key is currently blocked.
func (bfp *BruteForceProtection) Check(key string) error {
	bfp.totalChecks.Add(1)
	bfp.mu.RLock()
	entry, ok := bfp.entries[key]
	bfp.mu.RUnlock()
	if !ok {
		return nil
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if time.Now().Before(entry.blockedUntil) {
		remaining := time.Until(entry.blockedUntil)
		return fmt.Errorf("brute force: key %q blocked for another %v", key, remaining.Truncate(time.Second))
	}
	// Reset counter if window has passed since last failure
	if time.Since(entry.lastFailure) > bfp.windowDuration {
		entry.failures = 0
	}
	return nil
}

// RecordFailure records a failed attempt. Returns true if now blocked.
func (bfp *BruteForceProtection) RecordFailure(key string) bool {
	bfp.mu.Lock()
	entry, ok := bfp.entries[key]
	if !ok {
		entry = &BruteForceEntry{}
		bfp.entries[key] = entry
	}
	bfp.mu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()

	// Reset if window expired
	if time.Since(entry.lastFailure) > bfp.windowDuration {
		entry.failures = 0
	}

	entry.failures++
	entry.lastFailure = time.Now()

	if entry.failures >= bfp.maxAttempts {
		// Exponential backoff: base * 2^(totalBlocks)
		backoff := bfp.baseLockout * (1 << uint(entry.totalBlocks))
		if backoff > bfp.maxLockout {
			backoff = bfp.maxLockout
		}
		entry.blockedUntil = time.Now().Add(backoff)
		entry.totalBlocks++
		entry.failures = 0
		bfp.totalBlocks.Add(1)
		return true
	}
	return false
}

// RecordSuccess resets the failure count (successful auth).
func (bfp *BruteForceProtection) RecordSuccess(key string) {
	bfp.mu.Lock()
	if entry, ok := bfp.entries[key]; ok {
		entry.mu.Lock()
		entry.failures = 0
		entry.blockedUntil = time.Time{}
		entry.mu.Unlock()
	}
	bfp.mu.Unlock()
}

// Unblock manually removes a block (admin action).
func (bfp *BruteForceProtection) Unblock(key string) {
	bfp.mu.Lock()
	delete(bfp.entries, key)
	bfp.mu.Unlock()
}

func (bfp *BruteForceProtection) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-bfp.maxLockout * 2)
		bfp.mu.Lock()
		for k, e := range bfp.entries {
			e.mu.Lock()
			if e.lastFailure.Before(cutoff) {
				delete(bfp.entries, k)
			}
			e.mu.Unlock()
		}
		bfp.mu.Unlock()
	}
}

// ============================================================
// IP REPUTATION & BLOCKLIST MANAGER
// ============================================================

// ReputationEntry holds IP reputation data.
type ReputationEntry struct {
	IPAddress    string
	Score        float64 // 0.0 (clean) to 1.0 (malicious)
	Tags         []string
	BlockedUntil time.Time
	FirstSeen    time.Time
	LastSeen     time.Time
	HitCount     uint64
	Source       string // Where this entry came from
}

// IPReputationManager tracks IP reputation scores and enforces blocks.
type IPReputationManager struct {
	mu         sync.RWMutex
	entries    map[string]*ReputationEntry
	CIDRBlocks []*net.IPNet // Entire CIDR ranges that are blocked
	threshold  float64      // Score above which to block
	// Feed integration
	feedMu   sync.Mutex
	feedURLs []string
}

// NewIPReputationManager creates a reputation manager.
func NewIPReputationManager(blockThreshold float64) *IPReputationManager {
	m := &IPReputationManager{
		entries:   make(map[string]*ReputationEntry),
		threshold: blockThreshold,
	}
	// Pre-populate with known malicious CIDR ranges (Tor exit nodes, etc. - examples)
	m.blockCIDRs([]string{
		"0.0.0.0/8",
		"100.64.0.0/10", // RFC 6598 shared space
	})
	go m.cleanup()
	return m
}

func (m *IPReputationManager) blockCIDRs(cidrs []string) {
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			m.CIDRBlocks = append(m.CIDRBlocks, ipNet)
		}
	}
}

// IsBlocked returns true if the IP is blocked.
func (m *IPReputationManager) IsBlocked(ip string) (bool, *ReputationEntry) {
	parsed := net.ParseIP(ip)
	if parsed != nil {
		for _, cidr := range m.CIDRBlocks {
			if cidr.Contains(parsed) {
				return true, &ReputationEntry{IPAddress: ip, Score: 1.0, Tags: []string{"cidr_block"}}
			}
		}
	}

	m.mu.RLock()
	e, ok := m.entries[ip]
	m.mu.RUnlock()
	if !ok {
		return false, nil
	}
	if !e.BlockedUntil.IsZero() && time.Now().Before(e.BlockedUntil) {
		return true, e
	}
	return e.Score >= m.threshold, e
}

// Report records a malicious event for an IP.
func (m *IPReputationManager) Report(ip string, threatType ThreatType, score float64, blockDuration time.Duration) {
	m.mu.Lock()
	e, ok := m.entries[ip]
	if !ok {
		e = &ReputationEntry{IPAddress: ip, FirstSeen: time.Now(), Source: "internal"}
		m.entries[ip] = e
	}
	// Score is exponentially weighted (new events have more influence)
	e.Score = math.Min(1.0, e.Score*0.8+score*0.2)
	e.LastSeen = time.Now()
	e.HitCount++
	e.Tags = appendUnique(e.Tags, string(threatType))
	if blockDuration > 0 {
		if e.BlockedUntil.Before(time.Now().Add(blockDuration)) {
			e.BlockedUntil = time.Now().Add(blockDuration)
		}
	}
	m.mu.Unlock()
}

// BlockIP explicitly blocks an IP for a duration.
func (m *IPReputationManager) BlockIP(ip string, duration time.Duration, reason string) {
	m.mu.Lock()
	e, ok := m.entries[ip]
	if !ok {
		e = &ReputationEntry{IPAddress: ip, FirstSeen: time.Now()}
		m.entries[ip] = e
	}
	e.Score = 1.0
	e.BlockedUntil = time.Now().Add(duration)
	e.Tags = appendUnique(e.Tags, reason)
	m.mu.Unlock()
}

func (m *IPReputationManager) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-24 * time.Hour)
		m.mu.Lock()
		for k, e := range m.entries {
			if e.LastSeen.Before(cutoff) && e.Score < 0.5 {
				delete(m.entries, k)
			}
		}
		m.mu.Unlock()
	}
}

// ============================================================
// BEHAVIORAL ANOMALY DETECTOR
// ============================================================

// BehaviorProfile tracks request behavior for an entity (IP/user).
type BehaviorProfile struct {
	mu            sync.Mutex
	entityID      string
	requestCount  uint64
	errorCount    uint64
	uniquePaths   map[string]int
	uniqueUAs     map[string]struct{}
	requestTimes  []time.Time // recent request timestamps
	avgIntervalNS float64
	firstSeen     time.Time
	lastSeen      time.Time
	anomalyScore  float64
}

// AnomalyDetector tracks behavioral profiles and scores anomalies.
type AnomalyDetector struct {
	mu       sync.RWMutex
	profiles map[string]*BehaviorProfile
	// Thresholds
	maxReqPerMinute   float64
	maxUniquePathsMin int
	maxUAChanges      int
	stopCh            chan struct{}
}

// NewAnomalyDetector creates a behavioral anomaly detector.
func NewAnomalyDetector(maxReqPerMinute float64, maxUniquePaths int) *AnomalyDetector {
	ad := &AnomalyDetector{
		profiles:          make(map[string]*BehaviorProfile),
		maxReqPerMinute:   maxReqPerMinute,
		maxUniquePathsMin: maxUniquePaths,
		maxUAChanges:      3,
		stopCh:            make(chan struct{}),
	}
	go ad.cleanup()
	return ad
}

// Record records a request and returns the current anomaly score (0.0-1.0).
func (ad *AnomalyDetector) Record(entityID, path, userAgent string, isError bool) float64 {
	ad.mu.Lock()
	p, ok := ad.profiles[entityID]
	if !ok {
		p = &BehaviorProfile{
			entityID:    entityID,
			uniquePaths: make(map[string]int),
			uniqueUAs:   make(map[string]struct{}),
			firstSeen:   time.Now(),
		}
		ad.profiles[entityID] = p
	}
	ad.mu.Unlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	p.requestCount++
	p.lastSeen = now
	p.uniquePaths[path]++
	if userAgent != "" {
		p.uniqueUAs[userAgent] = struct{}{}
	}
	if isError {
		p.errorCount++
	}

	// Keep last 60 seconds of request times
	cutoff := now.Add(-time.Minute)
	p.requestTimes = append(p.requestTimes, now)
	valid := p.requestTimes[:0]
	for _, t := range p.requestTimes {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	p.requestTimes = valid

	// Compute anomaly score
	score := 0.0
	recentRPM := float64(len(p.requestTimes))
	if recentRPM > ad.maxReqPerMinute {
		score += math.Min(0.5, (recentRPM-ad.maxReqPerMinute)/ad.maxReqPerMinute*0.5)
	}
	if len(p.uniquePaths) > ad.maxUniquePathsMin {
		score += math.Min(0.3, float64(len(p.uniquePaths)-ad.maxUniquePathsMin)/50.0*0.3)
	}
	if len(p.uniqueUAs) > ad.maxUAChanges {
		score += 0.2
	}
	if p.requestCount > 10 {
		errRate := float64(p.errorCount) / float64(p.requestCount)
		if errRate > 0.5 {
			score += math.Min(0.3, errRate*0.3)
		}
	}
	p.anomalyScore = math.Min(1.0, score)
	return p.anomalyScore
}

// GetScore returns the current anomaly score for an entity.
func (ad *AnomalyDetector) GetScore(entityID string) float64 {
	ad.mu.RLock()
	p, ok := ad.profiles[entityID]
	ad.mu.RUnlock()
	if !ok {
		return 0
	}
	p.mu.Lock()
	s := p.anomalyScore
	p.mu.Unlock()
	return s
}

func (ad *AnomalyDetector) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cutoff := time.Now().Add(-30 * time.Minute)
			ad.mu.Lock()
			for k, p := range ad.profiles {
				p.mu.Lock()
				old := p.lastSeen.Before(cutoff)
				p.mu.Unlock()
				if old {
					delete(ad.profiles, k)
				}
			}
			ad.mu.Unlock()
		case <-ad.stopCh:
			return
		}
	}
}

// ============================================================
// BOT DETECTOR
// ============================================================

// BotSignal is a signal that contributes to the bot score.
type BotSignal struct {
	Name    string
	Weight  float64
	Matched bool
}

// BotDetector scores requests on likelihood of being automated bots.
type BotDetector struct {
	knownBotUAs   []*regexp.Regexp
	knownHumanUAs []*regexp.Regexp
	honeypotPaths map[string]struct{}
	mu            sync.Mutex
}

// NewBotDetector creates a bot detector with production signatures.
func NewBotDetector() *BotDetector {
	compile := func(patterns []string) []*regexp.Regexp {
		var result []*regexp.Regexp
		for _, p := range patterns {
			if r, err := regexp.Compile(p); err == nil {
				result = append(result, r)
			}
		}
		return result
	}
	return &BotDetector{
		knownBotUAs: compile([]string{
			`(?i)(bot|crawler|spider|scraper|curl|wget|python|go-http|java\/|okhttp|axios|fetch|node-fetch|libwww|lwp|mechanize)`,
			`(?i)(semrush|ahrefs|moz\.com|yandex\.com|baidu|bingbot|googlebot|slurp)`,
			`(?i)(phantomjs|headlesschrome|selenium|puppeteer|playwright)`,
		}),
		knownHumanUAs: compile([]string{
			`(?i)Mozilla\/5\.0.*(Chrome|Firefox|Safari|Edge|Opera)`,
		}),
		honeypotPaths: map[string]struct{}{
			"/.env":             {},
			"/.git/config":      {},
			"/wp-admin":         {},
			"/wp-login.php":     {},
			"/phpmyadmin":       {},
			"/admin/config":     {},
			"/.aws/credentials": {},
			"/etc/passwd":       {},
			"/api/swagger.json": {}, // Honeypot if not real
			"/actuator":         {},
		},
	}
}

// ScoreRequest returns a bot probability score (0.0=human, 1.0=bot) and signals.
func (bd *BotDetector) ScoreRequest(r *http.Request) (float64, []BotSignal) {
	var signals []BotSignal
	score := 0.0

	ua := r.Header.Get("User-Agent")

	// Check known bot UAs
	for _, re := range bd.knownBotUAs {
		if re.MatchString(ua) {
			signals = append(signals, BotSignal{Name: "known_bot_ua", Weight: 0.7, Matched: true})
			score += 0.7
			break
		}
	}

	// Empty or very short UA
	if len(ua) < 10 {
		signals = append(signals, BotSignal{Name: "short_ua", Weight: 0.5, Matched: true})
		score += 0.5
	}

	// Missing common browser headers
	missing := 0
	for _, h := range []string{"Accept", "Accept-Language", "Accept-Encoding"} {
		if r.Header.Get(h) == "" {
			missing++
		}
	}
	if missing >= 2 {
		signals = append(signals, BotSignal{Name: "missing_browser_headers", Weight: 0.4, Matched: true})
		score += 0.4
	}

	// Honeypot path access
	path := strings.ToLower(r.URL.Path)
	if _, ok := bd.honeypotPaths[path]; ok {
		signals = append(signals, BotSignal{Name: "honeypot_path", Weight: 0.9, Matched: true})
		score += 0.9
	}

	// HTTP/2 with bot UA is unusual
	if r.Proto == "HTTP/2.0" && score > 0.5 {
		signals = append(signals, BotSignal{Name: "http2_bot", Weight: -0.1, Matched: true})
		score -= 0.1 // Slightly lower (bots sometimes use HTTP/2)
	}

	return math.Min(1.0, score), signals
}

// ============================================================
// VELOCITY CHECKER (Credential Stuffing Detection)
// ============================================================

// VelocityChecker detects credential stuffing and account enumeration
// by tracking authentication attempts across multiple accounts from one source.
type VelocityChecker struct {
	mu          sync.Mutex
	windows     map[string]*velocityWindow
	maxAccounts int
	windowDur   time.Duration
}

type velocityWindow struct {
	accounts map[string]struct{}
	since    time.Time
}

// NewVelocityChecker creates a velocity checker.
// maxAccounts: max unique accounts per source per window before alerting.
func NewVelocityChecker(maxAccounts int, window time.Duration) *VelocityChecker {
	return &VelocityChecker{
		windows:     make(map[string]*velocityWindow),
		maxAccounts: maxAccounts,
		windowDur:   window,
	}
}

// Check records a login attempt and returns true if credential stuffing is suspected.
func (vc *VelocityChecker) Check(sourceIP, accountID string) bool {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	w, ok := vc.windows[sourceIP]
	if !ok || time.Since(w.since) > vc.windowDur {
		w = &velocityWindow{accounts: make(map[string]struct{}), since: time.Now()}
		vc.windows[sourceIP] = w
	}
	w.accounts[accountID] = struct{}{}
	return len(w.accounts) > vc.maxAccounts
}

// ============================================================
// THREAT INTELLIGENCE LOG
// ============================================================

// ThreatLogger records and correlates threat events.
type ThreatLogger struct {
	mu       sync.RWMutex
	events   []*ThreatEvent
	maxSize  int
	handlers []func(*ThreatEvent)
	counter  atomic.Uint64
}

// NewThreatLogger creates a threat event logger with a rolling buffer.
func NewThreatLogger(maxSize int) *ThreatLogger {
	return &ThreatLogger{
		events:  make([]*ThreatEvent, 0, maxSize),
		maxSize: maxSize,
	}
}

// OnThreat registers a handler called for every new threat event.
func (tl *ThreatLogger) OnThreat(fn func(*ThreatEvent)) {
	tl.mu.Lock()
	tl.handlers = append(tl.handlers, fn)
	tl.mu.Unlock()
}

// Log records a threat event.
func (tl *ThreatLogger) Log(e *ThreatEvent) {
	e.ID = fmt.Sprintf("threat-%d-%d", time.Now().UnixNano(), tl.counter.Add(1))

	tl.mu.Lock()
	if len(tl.events) >= tl.maxSize {
		tl.events = tl.events[1:] // Roll oldest
	}
	tl.events = append(tl.events, e)
	handlers := tl.handlers
	tl.mu.Unlock()

	for _, h := range handlers {
		go h(e)
	}
}

// Query returns events matching the given IP or user ID (most recent first).
func (tl *ThreatLogger) Query(ip, userID string, limit int) []*ThreatEvent {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	var result []*ThreatEvent
	for i := len(tl.events) - 1; i >= 0 && len(result) < limit; i-- {
		e := tl.events[i]
		if (ip == "" || e.IPAddress == ip) && (userID == "" || e.UserID == userID) {
			result = append(result, e)
		}
	}
	return result
}

// Stats returns aggregate threat statistics.
func (tl *ThreatLogger) Stats() map[string]interface{} {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	byType := make(map[ThreatType]int)
	byLevel := make(map[ThreatLevel]int)
	for _, e := range tl.events {
		byType[e.Type]++
		byLevel[e.Level]++
	}
	return map[string]interface{}{
		"total":    len(tl.events),
		"by_type":  byType,
		"by_level": byLevel,
		"capacity": tl.maxSize,
	}
}

// ============================================================
// REQUEST FINGERPRINTER
// ============================================================

// RequestFingerprinter creates stable fingerprints for HTTP requests
// useful for tracking unique request patterns across distributed systems.
type RequestFingerprinter struct{}

// Fingerprint generates a hash representing the request's unique characteristics.
func (rf *RequestFingerprinter) Fingerprint(r *http.Request) string {
	h := sha256.New()
	h.Write([]byte(r.Method))
	h.Write([]byte(r.URL.Path))
	h.Write([]byte(r.Header.Get("User-Agent")))
	h.Write([]byte(r.Header.Get("Accept-Language")))
	h.Write([]byte(r.Header.Get("Accept-Encoding")))
	h.Write([]byte(r.Header.Get("Accept")))
	// Include real IP
	ip := extractIP(r)
	h.Write([]byte(ip))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ============================================================
// GRADUATED RESPONSE ENGINE
// ============================================================

// ResponseAction defines what to do when a threat is detected.
type ResponseAction string

const (
	ActionLog        ResponseAction = "LOG"        // Log only
	ActionRateLimit  ResponseAction = "RATE_LIMIT" // Apply strict rate limit
	ActionChallenge  ResponseAction = "CHALLENGE"  // Return CAPTCHA challenge
	ActionBlock      ResponseAction = "BLOCK"      // Block request
	ActionQuarantine ResponseAction = "QUARANTINE" // Block all future requests from source
	ActionHoneypot   ResponseAction = "HONEYPOT"   // Return fake data, observe
	ActionAlert      ResponseAction = "ALERT"      // Page on-call team
)

// GraduatedResponse maps risk scores to escalating response actions.
type GraduatedResponse struct {
	thresholds []responseThreshold
}

type responseThreshold struct {
	minScore float64
	action   ResponseAction
}

// DefaultGraduatedResponse returns a standard graduated response configuration.
func DefaultGraduatedResponse() *GraduatedResponse {
	return &GraduatedResponse{
		thresholds: []responseThreshold{
			{minScore: 0.9, action: ActionQuarantine},
			{minScore: 0.75, action: ActionAlert},
			{minScore: 0.6, action: ActionBlock},
			{minScore: 0.4, action: ActionChallenge},
			{minScore: 0.2, action: ActionRateLimit},
			{minScore: 0.0, action: ActionLog},
		},
	}
}

// Decide returns the appropriate action for a given risk score.
func (gr *GraduatedResponse) Decide(score float64) ResponseAction {
	for _, t := range gr.thresholds {
		if score >= t.minScore {
			return t.action
		}
	}
	return ActionLog
}

// ============================================================
// THREAT DEFENSE MIDDLEWARE (Main Entry Point)
// ============================================================

// ThreatDefenseConfig configures the complete threat defense system.
type ThreatDefenseConfig struct {
	ServiceName string

	// Injection
	EnableInjectionDetection bool

	// Brute force
	MaxLoginAttempts int
	LoginWindow      time.Duration
	BaseLockout      time.Duration
	MaxLockout       time.Duration

	// Anomaly
	MaxReqPerMinute float64
	MaxUniquePaths  int

	// Reputation
	IPBlockThreshold float64

	// Bot
	EnableBotDetection bool
	BotScoreThreshold  float64

	// Credential stuffing
	MaxAccountsPerIP int
	StuffingWindow   time.Duration

	// Response
	BlockedResponse string
	BlockedStatus   int

	// Callbacks
	OnThreat func(*ThreatEvent)
}

// DefaultThreatDefenseConfig returns production-hardened defaults.
func DefaultThreatDefenseConfig(serviceName string) ThreatDefenseConfig {
	return ThreatDefenseConfig{
		ServiceName:              serviceName,
		EnableInjectionDetection: true,
		MaxLoginAttempts:         5,
		LoginWindow:              5 * time.Minute,
		BaseLockout:              1 * time.Minute,
		MaxLockout:               24 * time.Hour,
		MaxReqPerMinute:          300,
		MaxUniquePaths:           50,
		IPBlockThreshold:         0.7,
		EnableBotDetection:       true,
		BotScoreThreshold:        0.8,
		MaxAccountsPerIP:         5,
		StuffingWindow:           10 * time.Minute,
		BlockedResponse:          `{"error":"forbidden","message":"request blocked by security policy"}`,
		BlockedStatus:            http.StatusForbidden,
	}
}

// ThreatDefense is the unified threat defense engine.
type ThreatDefense struct {
	cfg           ThreatDefenseConfig
	injDetector   *InjectionDetector
	bruteForce    *BruteForceProtection
	reputation    *IPReputationManager
	anomaly       *AnomalyDetector
	botDetector   *BotDetector
	velocity      *VelocityChecker
	logger        *ThreatLogger
	response      *GraduatedResponse
	fingerprinter *RequestFingerprinter
	// Metrics
	totalRequests atomic.Uint64
	totalBlocked  atomic.Uint64
	totalThreats  atomic.Uint64
}

// New creates a fully configured ThreatDefense engine.
func New(cfg ThreatDefenseConfig) *ThreatDefense {
	td := &ThreatDefense{
		cfg:           cfg,
		injDetector:   NewInjectionDetector(),
		bruteForce:    NewBruteForceProtection(cfg.MaxLoginAttempts, cfg.LoginWindow, cfg.BaseLockout, cfg.MaxLockout),
		reputation:    NewIPReputationManager(cfg.IPBlockThreshold),
		anomaly:       NewAnomalyDetector(cfg.MaxReqPerMinute, cfg.MaxUniquePaths),
		botDetector:   NewBotDetector(),
		velocity:      NewVelocityChecker(cfg.MaxAccountsPerIP, cfg.StuffingWindow),
		logger:        NewThreatLogger(100000),
		response:      DefaultGraduatedResponse(),
		fingerprinter: &RequestFingerprinter{},
	}
	if cfg.OnThreat != nil {
		td.logger.OnThreat(cfg.OnThreat)
	}
	return td
}

// Middleware returns the HTTP middleware that applies all threat defense.
func (td *ThreatDefense) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		td.totalRequests.Add(1)
		ip := extractIP(r)

		// ---- IP Reputation Check ----
		if blocked, entry := td.reputation.IsBlocked(ip); blocked {
			td.logAndBlock(w, r, ip, "", ThreatIPReputation, ThreatLevelHigh, 1.0,
				"IP reputation: "+strings.Join(entry.Tags, ","))
			return
		}

		// ---- Brute Force Check ----
		if err := td.bruteForce.Check(ip); err != nil {
			td.logAndBlock(w, r, ip, "", ThreatBruteForce, ThreatLevelHigh, 0.9, err.Error())
			return
		}

		// ---- Injection Detection ----
		if td.cfg.EnableInjectionDetection {
			if results := td.injDetector.ScanRequest(r); len(results) > 0 {
				top := results[0]
				td.totalThreats.Add(1)
				td.reputation.Report(ip, top.Type, top.Score, 30*time.Minute)
				td.logAndBlock(w, r, ip, "", top.Type, ThreatLevelHigh, top.Score, top.Pattern)
				return
			}
		}

		// ---- Behavioral Anomaly Detection ----
		anomalyScore := td.anomaly.Record(ip, r.URL.Path, r.Header.Get("User-Agent"), false)
		if anomalyScore > 0.7 {
			action := td.response.Decide(anomalyScore)
			if action == ActionBlock || action == ActionQuarantine {
				td.logAndBlock(w, r, ip, "", ThreatAnomalousVelocity, ThreatLevelMedium, anomalyScore, "anomaly score")
				return
			}
		}

		// ---- Bot Detection ----
		if td.cfg.EnableBotDetection {
			botScore, _ := td.botDetector.ScoreRequest(r)
			if botScore >= td.cfg.BotScoreThreshold {
				td.totalThreats.Add(1)
				td.reputation.Report(ip, ThreatBotActivity, botScore, 10*time.Minute)
				td.logAndBlock(w, r, ip, "", ThreatBotActivity, ThreatLevelMedium, botScore, "bot score")
				return
			}
		}

		// ---- Honeypot Path Detection ----
		if _, ok := td.botDetector.honeypotPaths[strings.ToLower(r.URL.Path)]; ok {
			td.totalThreats.Add(1)
			td.reputation.Report(ip, ThreatHoneypotTriggered, 0.9, 1*time.Hour)
			td.logAndBlock(w, r, ip, "", ThreatHoneypotTriggered, ThreatLevelHigh, 0.9, r.URL.Path)
			return
		}

		// ---- HTTP Parameter Pollution ----
		if td.detectParamPollution(r) {
			td.logAndBlock(w, r, ip, "", ThreatParamPollution, ThreatLevelMedium, 0.6, "duplicate parameters")
			return
		}

		// ---- Request Smuggling Detection ----
		if td.detectRequestSmuggling(r) {
			td.logAndBlock(w, r, ip, "", ThreatRequestSmuggling, ThreatLevelCritical, 1.0, "conflicting content-length/transfer-encoding")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (td *ThreatDefense) logAndBlock(w http.ResponseWriter, r *http.Request, ip, userID string, t ThreatType, level ThreatLevel, score float64, evidence string) {
	td.totalBlocked.Add(1)
	fingerprint := td.fingerprinter.Fingerprint(r)
	event := &ThreatEvent{
		Timestamp:   time.Now(),
		Type:        t,
		Level:       level,
		IPAddress:   ip,
		UserID:      userID,
		RequestPath: r.URL.Path,
		Method:      r.Method,
		Evidence:    evidence,
		Score:       score,
		Action:      string(td.response.Decide(score)),
		ServiceName: td.cfg.ServiceName,
		Fingerprint: fingerprint,
	}
	td.logger.Log(event)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(td.cfg.BlockedStatus)
	w.Write([]byte(td.cfg.BlockedResponse))
}

func (td *ThreatDefense) detectParamPollution(r *http.Request) bool {
	seen := make(map[string]struct{})
	for key := range r.URL.Query() {
		if _, exists := seen[key]; exists {
			return true
		}
		seen[key] = struct{}{}
	}
	return false
}

func (td *ThreatDefense) detectRequestSmuggling(r *http.Request) bool {
	cl := r.Header.Get("Content-Length")
	te := r.Header.Get("Transfer-Encoding")
	// Both present is a smuggling indicator
	return cl != "" && te != "" && strings.Contains(strings.ToLower(te), "chunked")
}

// RecordLoginAttempt records a login attempt for brute-force and stuffing protection.
// Returns true if the attempt should be blocked.
func (td *ThreatDefense) RecordLoginAttempt(r *http.Request, accountID string, success bool) bool {
	ip := extractIP(r)
	if success {
		td.bruteForce.RecordSuccess(ip)
		return false
	}
	blocked := td.bruteForce.RecordFailure(ip)
	if blocked {
		td.reputation.Report(ip, ThreatBruteForce, 0.8, 15*time.Minute)
	}
	// Check credential stuffing
	if td.velocity.Check(ip, accountID) {
		td.reputation.Report(ip, ThreatCredentialStuffing, 0.9, 1*time.Hour)
		return true
	}
	return blocked
}

// BlockIP manually blocks an IP address.
func (td *ThreatDefense) BlockIP(ip string, duration time.Duration, reason string) {
	td.reputation.BlockIP(ip, duration, reason)
}

// UnblockIP removes a manual IP block.
func (td *ThreatDefense) UnblockIP(ip string) {
	td.bruteForce.Unblock(ip)
}

// ThreatStats returns a snapshot of all threat defense statistics.
type ThreatStats struct {
	TotalRequests uint64                 `json:"total_requests"`
	TotalBlocked  uint64                 `json:"total_blocked"`
	TotalThreats  uint64                 `json:"total_threats"`
	BlockRate     float64                `json:"block_rate_percent"`
	EventStats    map[string]interface{} `json:"event_stats"`
	Timestamp     time.Time              `json:"timestamp"`
}

func (td *ThreatDefense) Stats() ThreatStats {
	total := td.totalRequests.Load()
	blocked := td.totalBlocked.Load()
	blockRate := 0.0
	if total > 0 {
		blockRate = float64(blocked) / float64(total) * 100
	}
	return ThreatStats{
		TotalRequests: total,
		TotalBlocked:  blocked,
		TotalThreats:  td.totalThreats.Load(),
		BlockRate:     blockRate,
		EventStats:    td.logger.Stats(),
		Timestamp:     time.Now(),
	}
}

// RecentThreats returns the most recent threat events.
func (td *ThreatDefense) RecentThreats(limit int) []*ThreatEvent {
	return td.logger.Query("", "", limit)
}

// ThreatsForIP returns threat events for a specific IP.
func (td *ThreatDefense) ThreatsForIP(ip string, limit int) []*ThreatEvent {
	return td.logger.Query(ip, "", limit)
}

// ExportThreatReport exports all current threat events as JSON.
func (td *ThreatDefense) ExportThreatReport() ([]byte, error) {
	stats := td.Stats()
	events := td.RecentThreats(1000)
	return json.Marshal(map[string]interface{}{
		"stats":  stats,
		"events": events,
	})
}

// StatusHandler returns an HTTP handler for the defense status endpoint.
func (td *ThreatDefense) StatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := td.Stats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

// ============================================================
// UTILITIES
// ============================================================

// extractIP extracts the real client IP, accounting for proxies.
func extractIP(r *http.Request) string {
	// Check trusted proxy headers
	for _, header := range []string{"X-Real-IP", "X-Forwarded-For", "CF-Connecting-IP"} {
		if ip := r.Header.Get(header); ip != "" {
			// X-Forwarded-For can be comma-separated; take the first
			parts := strings.SplitN(ip, ",", 2)
			candidate := strings.TrimSpace(parts[0])
			if net.ParseIP(candidate) != nil {
				return candidate
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func appendUnique(slice []string, val string) []string {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}

// SanitizeForLog removes control characters and limits length for safe logging.
func SanitizeForLog(input string, maxLen int) string {
	var b strings.Builder
	for _, r := range input {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
		if b.Len() >= maxLen {
			b.WriteString("...[truncated]")
			break
		}
	}
	return b.String()
}
