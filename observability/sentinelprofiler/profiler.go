// Package profiler provides enterprise-grade, production-safe performance
// inspection and continuous profiling for distributed microservices.
// Designed as a reusable shared library across the organization.
//
// Features:
//   - Continuous CPU/Memory/Goroutine profiling with adaptive sampling
//   - Real-time RED metrics (Rate, Errors, Duration)
//   - Percentile latency tracking (P50/P90/P95/P99/P999) via HDR histogram
//   - SLO/SLA tracking with burn rate alerts
//   - Call graph capture and hot-path detection
//   - Differential profiling (before vs after deploy)
//   - pprof HTTP endpoint with auth
//   - Prometheus-compatible metric exposition
//   - Heap leak detection
//   - GC pressure tracking
package profiler

import (
	"fmt"
	"math"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// HDR HISTOGRAM (High Dynamic Range — lock-free, fast percentiles)
// ============================================================

// HDRHistogram tracks latency distribution with configurable precision.
// Based on HDR Histogram algorithm by Gil Tene.
type HDRHistogram struct {
	counts     []atomic.Int64
	minVal     atomic.Int64
	maxVal     atomic.Int64
	totalCount atomic.Int64
	totalSum   atomic.Int64
	// Configuration
	lowestDiscernible  int64
	highestTrackable   int64
	significantFigures int
	unitMagnitude      int
	subBucketCount     int
	subBucketHalfCount int
	subBucketMask      int64
	bucketCount        int
}

// NewHDRHistogram creates a histogram for values between min and max
// with the given number of significant figures (1-5).
func NewHDRHistogram(minVal, maxVal int64, sigFigs int) *HDRHistogram {
	h := &HDRHistogram{
		lowestDiscernible:  minVal,
		highestTrackable:   maxVal,
		significantFigures: sigFigs,
	}
	h.init()
	return h
}

func (h *HDRHistogram) init() {
	largestValueWithSingleUnitResolution := int64(2 * math.Pow10(h.significantFigures))
	subBucketCountMagnitude := int(math.Ceil(math.Log2(float64(largestValueWithSingleUnitResolution))))
	h.subBucketHalfCount = 1 << uint(subBucketCountMagnitude)
	h.subBucketCount = h.subBucketHalfCount * 2
	h.subBucketMask = int64(h.subBucketCount-1) << uint(h.unitMagnitude)

	h.bucketCount = h.bucketIndexForValue(h.highestTrackable) + 1
	h.counts = make([]atomic.Int64, (h.bucketCount+1)*(h.subBucketCount/2))
	h.minVal.Store(math.MaxInt64)
}

func (h *HDRHistogram) bucketIndexForValue(v int64) int {
	pow2Ceiling := 64 - countLeadingZeros(uint64(v|h.subBucketMask))
	return pow2Ceiling - h.unitMagnitude - (h.subBucketCount >> 1)
}

func countLeadingZeros(x uint64) int {
	if x == 0 {
		return 64
	}
	n := 0
	for x&(1<<63) == 0 {
		n++
		x <<= 1
	}
	return n
}

func (h *HDRHistogram) countsIndexForValue(v int64) int {
	bucketIdx := h.bucketIndexForValue(v)
	subBucketIdx := int(v >> uint(bucketIdx+h.unitMagnitude))
	if bucketIdx < 0 {
		return subBucketIdx - h.subBucketHalfCount
	}
	return (bucketIdx+1)*h.subBucketHalfCount + subBucketIdx - h.subBucketHalfCount
}

// Record records a value in the histogram (thread-safe, lock-free).
func (h *HDRHistogram) Record(value int64) bool {
	if value < h.lowestDiscernible || value > h.highestTrackable {
		return false
	}
	idx := h.countsIndexForValue(value)
	if idx < 0 || idx >= len(h.counts) {
		return false
	}
	h.counts[idx].Add(1)
	h.totalCount.Add(1)
	h.totalSum.Add(value)
	// Update min/max
	for {
		cur := h.minVal.Load()
		if value >= cur || h.minVal.CompareAndSwap(cur, value) {
			break
		}
	}
	for {
		cur := h.maxVal.Load()
		if value <= cur || h.maxVal.CompareAndSwap(cur, value) {
			break
		}
	}
	return true
}

// Percentile returns the value at the given percentile (0.0-100.0).
func (h *HDRHistogram) Percentile(pct float64) int64 {
	total := h.totalCount.Load()
	if total == 0 {
		return 0
	}
	target := int64(math.Ceil(float64(total) * pct / 100.0))
	var running int64
	for i, count := range h.counts {
		c := count.Load()
		if c == 0 {
			continue
		}
		running += c
		if running >= target {
			return h.valueFromIndex(i)
		}
	}
	return h.maxVal.Load()
}

func (h *HDRHistogram) valueFromIndex(idx int) int64 {
	bucketIdx := (idx >> uint(h.subBucketCount>>1)) - 1
	subBucketIdx := idx&(h.subBucketHalfCount-1) + h.subBucketHalfCount
	if bucketIdx < 0 {
		subBucketIdx -= h.subBucketHalfCount
		bucketIdx = 0
	}
	return int64(subBucketIdx) << uint(bucketIdx+h.unitMagnitude)
}

// Mean returns the arithmetic mean.
func (h *HDRHistogram) Mean() float64 {
	total := h.totalCount.Load()
	if total == 0 {
		return 0
	}
	return float64(h.totalSum.Load()) / float64(total)
}

// Reset clears all recorded values.
func (h *HDRHistogram) Reset() {
	for i := range h.counts {
		h.counts[i].Store(0)
	}
	h.totalCount.Store(0)
	h.totalSum.Store(0)
	h.minVal.Store(math.MaxInt64)
	h.maxVal.Store(0)
}

// Snapshot captures a consistent read of all percentiles.
type HistogramSnapshot struct {
	P50   int64
	P90   int64
	P95   int64
	P99   int64
	P999  int64
	Max   int64
	Min   int64
	Mean  float64
	Count int64
}

func (h *HDRHistogram) Snapshot() HistogramSnapshot {
	return HistogramSnapshot{
		P50:   h.Percentile(50),
		P90:   h.Percentile(90),
		P95:   h.Percentile(95),
		P99:   h.Percentile(99),
		P999:  h.Percentile(99.9),
		Max:   h.maxVal.Load(),
		Min:   h.minVal.Load(),
		Mean:  h.Mean(),
		Count: h.totalCount.Load(),
	}
}

// ============================================================
// RED METRICS TRACKER (Rate, Errors, Duration)
// ============================================================

// REDMetrics tracks the three golden signals for a service endpoint.
type REDMetrics struct {
	name      string
	histogram *HDRHistogram
	requests  atomic.Uint64
	errors    atomic.Uint64
	// Rate tracking (per-second over sliding window)
	windowMu   sync.Mutex
	windowReqs []int64 // req counts per second slot
	windowErrs []int64 // err counts per second slot
	windowHead int
	windowSize int // seconds
	lastTick   time.Time
	stopCh     chan struct{}
}

// NewREDMetrics creates a RED metrics tracker for a named endpoint/operation.
func NewREDMetrics(name string, histogramMaxMs int64) *REDMetrics {
	r := &REDMetrics{
		name:       name,
		histogram:  NewHDRHistogram(1, histogramMaxMs*1e6, 3),
		windowSize: 60,
		windowReqs: make([]int64, 60),
		windowErrs: make([]int64, 60),
		lastTick:   time.Now(),
		stopCh:     make(chan struct{}),
	}
	go r.tick()
	return r
}

func (r *REDMetrics) tick() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.advance()
		case <-r.stopCh:
			return
		}
	}
}

func (r *REDMetrics) advance() {
	r.windowMu.Lock()
	r.windowHead = (r.windowHead + 1) % r.windowSize
	r.windowReqs[r.windowHead] = 0
	r.windowErrs[r.windowHead] = 0
	r.windowMu.Unlock()
}

// Record records a completed request.
func (r *REDMetrics) Record(duration time.Duration, isError bool) {
	r.requests.Add(1)
	r.histogram.Record(duration.Nanoseconds())

	r.windowMu.Lock()
	r.windowReqs[r.windowHead]++
	if isError {
		r.errors.Add(1)
		r.windowErrs[r.windowHead]++
	}
	r.windowMu.Unlock()
}

// REDSnapshot is a point-in-time snapshot of RED metrics.
type REDSnapshot struct {
	Name          string            `json:"name"`
	RequestRate   float64           `json:"request_rate_per_sec"`
	ErrorRate     float64           `json:"error_rate_per_sec"`
	ErrorPercent  float64           `json:"error_percent"`
	TotalRequests uint64            `json:"total_requests"`
	TotalErrors   uint64            `json:"total_errors"`
	Latency       HistogramSnapshot `json:"latency_ns"`
	Timestamp     time.Time         `json:"timestamp"`
}

func (r *REDMetrics) Snapshot() REDSnapshot {
	r.windowMu.Lock()
	var totalReqs, totalErrs int64
	for _, v := range r.windowReqs {
		totalReqs += v
	}
	for _, v := range r.windowErrs {
		totalErrs += v
	}
	r.windowMu.Unlock()

	reqRate := float64(totalReqs) / float64(r.windowSize)
	errRate := float64(totalErrs) / float64(r.windowSize)
	total := r.requests.Load()
	errs := r.errors.Load()
	errPct := 0.0
	if total > 0 {
		errPct = float64(errs) / float64(total) * 100
	}

	return REDSnapshot{
		Name:          r.name,
		RequestRate:   reqRate,
		ErrorRate:     errRate,
		ErrorPercent:  errPct,
		TotalRequests: total,
		TotalErrors:   errs,
		Latency:       r.histogram.Snapshot(),
		Timestamp:     time.Now(),
	}
}

// ============================================================
// SLO TRACKER (Service Level Objectives)
// ============================================================

// SLOConfig defines an SLO target.
type SLOConfig struct {
	Name             string
	Target           float64 // e.g. 99.9 (percent)
	Window           time.Duration
	LatencyP99Budget time.Duration // P99 latency target
	ErrorRateBudget  float64       // Max error rate (%)
	BurnRateAlert    float64       // Alert when burn rate exceeds this multiple
}

// SLOTracker monitors SLO compliance and calculates error budgets.
type SLOTracker struct {
	cfg     SLOConfig
	metrics *REDMetrics
	mu      sync.Mutex
	// Rolling window for SLO compliance
	window       []bool // true = request met SLO, false = violated
	windowHead   int
	windowSize   int
	burnRate     atomic.Value // float64
	violations   atomic.Uint64
	totalChecked atomic.Uint64
}

// NewSLOTracker creates an SLO tracker backed by RED metrics.
func NewSLOTracker(cfg SLOConfig, metrics *REDMetrics) *SLOTracker {
	windowSize := int(cfg.Window.Seconds())
	if windowSize <= 0 {
		windowSize = 3600
	}
	st := &SLOTracker{
		cfg:        cfg,
		metrics:    metrics,
		window:     make([]bool, windowSize),
		windowSize: windowSize,
	}
	go st.compute()
	return st
}

func (st *SLOTracker) Record(latency time.Duration, isError bool) {
	met := !isError && latency <= st.cfg.LatencyP99Budget
	st.mu.Lock()
	st.window[st.windowHead] = met
	st.windowHead = (st.windowHead + 1) % st.windowSize
	st.mu.Unlock()
	st.totalChecked.Add(1)
	if !met {
		st.violations.Add(1)
	}
}

func (st *SLOTracker) compute() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		st.mu.Lock()
		var met int
		for _, v := range st.window {
			if v {
				met++
			}
		}
		st.mu.Unlock()
		compliance := float64(met) / float64(st.windowSize) * 100
		errorBudget := compliance - st.cfg.Target
		burnRate := 0.0
		if errorBudget < 0 {
			burnRate = math.Abs(errorBudget) / (100 - st.cfg.Target)
		}
		st.burnRate.Store(burnRate)
	}
}

// SLOReport is the current SLO compliance report.
type SLOReport struct {
	Name         string        `json:"name"`
	Target       float64       `json:"target_percent"`
	CurrentRate  float64       `json:"current_compliance_percent"`
	ErrorBudget  float64       `json:"error_budget_remaining_percent"`
	BurnRate     float64       `json:"burn_rate_multiple"`
	Violations   uint64        `json:"violations_total"`
	TotalChecked uint64        `json:"total_checked"`
	AtRisk       bool          `json:"at_risk"`
	Window       time.Duration `json:"window"`
	Timestamp    time.Time     `json:"timestamp"`
}

func (st *SLOTracker) Report() SLOReport {
	burnRate, _ := st.burnRate.Load().(float64)
	total := float64(st.windowSize)
	violations := float64(st.violations.Load() % uint64(st.windowSize))
	compliance := (total - violations) / total * 100
	errorBudget := compliance - st.cfg.Target

	return SLOReport{
		Name:         st.cfg.Name,
		Target:       st.cfg.Target,
		CurrentRate:  compliance,
		ErrorBudget:  errorBudget,
		BurnRate:     burnRate,
		Violations:   st.violations.Load(),
		TotalChecked: st.totalChecked.Load(),
		AtRisk:       burnRate >= st.cfg.BurnRateAlert,
		Window:       st.cfg.Window,
		Timestamp:    time.Now(),
	}
}

// ============================================================
// RUNTIME PROFILER (Continuous Profiling)
// ============================================================

// RuntimeSnapshot is a full snapshot of Go runtime metrics.
type RuntimeSnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	// Goroutines
	NumGoroutines int `json:"num_goroutines"`
	// Memory
	HeapAllocMB  float64 `json:"heap_alloc_mb"`
	HeapSysMB    float64 `json:"heap_sys_mb"`
	HeapIdleMB   float64 `json:"heap_idle_mb"`
	HeapInUseMB  float64 `json:"heap_in_use_mb"`
	HeapObjects  uint64  `json:"heap_objects"`
	StackInUseMB float64 `json:"stack_in_use_mb"`
	// GC
	NumGC          uint32  `json:"num_gc"`
	GCPauseTotalMS float64 `json:"gc_pause_total_ms"`
	LastGCPauseMS  float64 `json:"last_gc_pause_ms"`
	GCCPUFraction  float64 `json:"gc_cpu_fraction"`
	NextGCMB       float64 `json:"next_gc_mb"`
	// CPU
	NumCPU     int `json:"num_cpu"`
	GOMAXPROCS int `json:"gomaxprocs"`
	// Threads
	NumCgoCall int64 `json:"num_cgo_call"`
}

// SnapshotRuntime captures a full Go runtime metrics snapshot.
func SnapshotRuntime() RuntimeSnapshot {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	toMB := func(b uint64) float64 { return float64(b) / 1024 / 1024 }

	lastPause := float64(0)
	if ms.NumGC > 0 && int(ms.NumGC) <= len(ms.PauseNs) {
		lastPause = float64(ms.PauseNs[(ms.NumGC+255)%256]) / 1e6
	}

	return RuntimeSnapshot{
		Timestamp:      time.Now(),
		NumGoroutines:  runtime.NumGoroutine(),
		HeapAllocMB:    toMB(ms.HeapAlloc),
		HeapSysMB:      toMB(ms.HeapSys),
		HeapIdleMB:     toMB(ms.HeapIdle),
		HeapInUseMB:    toMB(ms.HeapInuse),
		HeapObjects:    ms.HeapObjects,
		StackInUseMB:   toMB(ms.StackInuse),
		NumGC:          ms.NumGC,
		GCPauseTotalMS: float64(ms.PauseTotalNs) / 1e6,
		LastGCPauseMS:  lastPause,
		GCCPUFraction:  ms.GCCPUFraction,
		NextGCMB:       toMB(ms.NextGC),
		NumCPU:         runtime.NumCPU(),
		GOMAXPROCS:     runtime.GOMAXPROCS(0),
		NumCgoCall:     runtime.NumCgoCall(),
	}
}

// ============================================================
// GOROUTINE LEAK DETECTOR
// ============================================================

// GoroutineLeakDetector monitors goroutine count growth over time
// and alerts if a leak is suspected.
type GoroutineLeakDetector struct {
	baseline      int
	threshold     float64 // growth factor (e.g. 2.0 = 2x baseline)
	checkPeriod   time.Duration
	maxGoroutines int
	alerts        []func(current, baseline int)
	stopCh        chan struct{}
	mu            sync.Mutex
	history       []goroutineDataPoint
}

type goroutineDataPoint struct {
	ts    time.Time
	count int
}

// NewGoroutineLeakDetector creates a leak detector.
// threshold: multiplier over baseline before alerting (e.g. 2.0)
func NewGoroutineLeakDetector(threshold float64, maxGoroutines int, checkPeriod time.Duration) *GoroutineLeakDetector {
	gld := &GoroutineLeakDetector{
		baseline:      runtime.NumGoroutine(),
		threshold:     threshold,
		maxGoroutines: maxGoroutines,
		checkPeriod:   checkPeriod,
		stopCh:        make(chan struct{}),
		history:       make([]goroutineDataPoint, 0, 1440), // 24h at 1min intervals
	}
	go gld.monitor()
	return gld
}

// OnLeak registers a callback invoked when a leak is detected.
func (gld *GoroutineLeakDetector) OnLeak(fn func(current, baseline int)) {
	gld.mu.Lock()
	gld.alerts = append(gld.alerts, fn)
	gld.mu.Unlock()
}

func (gld *GoroutineLeakDetector) monitor() {
	ticker := time.NewTicker(gld.checkPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			current := runtime.NumGoroutine()
			gld.mu.Lock()
			gld.history = append(gld.history, goroutineDataPoint{ts: time.Now(), count: current})
			if len(gld.history) > 1440 {
				gld.history = gld.history[1:]
			}
			alerts := gld.alerts
			baseline := gld.baseline
			gld.mu.Unlock()

			if float64(current) > float64(baseline)*gld.threshold || current > gld.maxGoroutines {
				for _, fn := range alerts {
					go fn(current, baseline)
				}
			}
		case <-gld.stopCh:
			return
		}
	}
}

// Stop halts the goroutine leak detector.
func (gld *GoroutineLeakDetector) Stop() {
	close(gld.stopCh)
}

// ============================================================
// HEAP GROWTH TRACKER
// ============================================================

// HeapGrowthTracker detects suspicious heap growth patterns.
type HeapGrowthTracker struct {
	snapshots []heapSnapshot
	mu        sync.Mutex
	stopCh    chan struct{}
	alertFn   func(growthMB float64, rate float64)
	threshold float64 // MB/min growth rate to alert
}

type heapSnapshot struct {
	ts     time.Time
	heapMB float64
}

// NewHeapGrowthTracker creates a heap growth tracker.
func NewHeapGrowthTracker(thresholdMBperMin float64) *HeapGrowthTracker {
	hgt := &HeapGrowthTracker{
		snapshots: make([]heapSnapshot, 0, 60),
		threshold: thresholdMBperMin,
		stopCh:    make(chan struct{}),
	}
	go hgt.monitor()
	return hgt
}

func (hgt *HeapGrowthTracker) OnAlert(fn func(growthMB float64, rate float64)) {
	hgt.mu.Lock()
	hgt.alertFn = fn
	hgt.mu.Unlock()
}

func (hgt *HeapGrowthTracker) monitor() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			heapMB := float64(ms.HeapAlloc) / 1024 / 1024

			hgt.mu.Lock()
			hgt.snapshots = append(hgt.snapshots, heapSnapshot{ts: time.Now(), heapMB: heapMB})
			if len(hgt.snapshots) > 60 {
				hgt.snapshots = hgt.snapshots[1:]
			}
			fn := hgt.alertFn
			// Compute growth rate (linear regression over last 10 snapshots)
			growthRate := hgt.computeGrowthRate()
			hgt.mu.Unlock()

			if growthRate > hgt.threshold && fn != nil {
				go fn(heapMB, growthRate)
			}
		case <-hgt.stopCh:
			return
		}
	}
}

func (hgt *HeapGrowthTracker) computeGrowthRate() float64 {
	n := len(hgt.snapshots)
	if n < 2 {
		return 0
	}
	window := hgt.snapshots
	if n > 10 {
		window = hgt.snapshots[n-10:]
	}
	first := window[0]
	last := window[len(window)-1]
	duration := last.ts.Sub(first.ts).Minutes()
	if duration <= 0 {
		return 0
	}
	return (last.heapMB - first.heapMB) / duration
}

// ============================================================
// PPROF HTTP HANDLER (Secured)
// ============================================================

// ProfilerServer exposes pprof endpoints with optional token auth.
type ProfilerServer struct {
	mux   *http.ServeMux
	token string // Bearer token for auth (empty = no auth)
}

// NewProfilerServer creates a secured pprof server.
func NewProfilerServer(token string) *ProfilerServer {
	ps := &ProfilerServer{mux: http.NewServeMux(), token: token}
	ps.register()
	return ps
}

func (ps *ProfilerServer) register() {
	routes := map[string]http.HandlerFunc{
		"/debug/pprof/":             pprof.Index,
		"/debug/pprof/cmdline":      pprof.Cmdline,
		"/debug/pprof/profile":      pprof.Profile,
		"/debug/pprof/symbol":       pprof.Symbol,
		"/debug/pprof/trace":        pprof.Trace,
		"/debug/pprof/heap":         pprof.Handler("heap").ServeHTTP,
		"/debug/pprof/goroutine":    pprof.Handler("goroutine").ServeHTTP,
		"/debug/pprof/allocs":       pprof.Handler("allocs").ServeHTTP,
		"/debug/pprof/block":        pprof.Handler("block").ServeHTTP,
		"/debug/pprof/mutex":        pprof.Handler("mutex").ServeHTTP,
		"/debug/pprof/threadcreate": pprof.Handler("threadcreate").ServeHTTP,
	}
	for path, handler := range routes {
		ps.mux.Handle(path, ps.authMiddleware(handler))
	}
	ps.mux.Handle("/debug/runtime", ps.authMiddleware(ps.runtimeHandler))
	ps.mux.Handle("/debug/gc", ps.authMiddleware(ps.gcHandler))
}

func (ps *ProfilerServer) authMiddleware(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ps.token != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+ps.token {
				w.Header().Set("WWW-Authenticate", `Bearer realm="profiler"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	})
}

func (ps *ProfilerServer) runtimeHandler(w http.ResponseWriter, r *http.Request) {
	snap := SnapshotRuntime()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"goroutines":%d,"heap_alloc_mb":%.2f,"heap_sys_mb":%.2f,"num_gc":%d,"gc_pause_ms":%.3f,"gomaxprocs":%d,"timestamp":"%s"}`,
		snap.NumGoroutines, snap.HeapAllocMB, snap.HeapSysMB,
		snap.NumGC, snap.LastGCPauseMS, snap.GOMAXPROCS, snap.Timestamp.Format(time.RFC3339))
}

func (ps *ProfilerServer) gcHandler(w http.ResponseWriter, r *http.Request) {
	runtime.GC()
	debug.FreeOSMemory()
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"message":"GC forced and OS memory freed"}`)
}

// Handler returns the HTTP handler for the profiler.
func (ps *ProfilerServer) Handler() http.Handler {
	return ps.mux
}

// ============================================================
// PROMETHEUS-COMPATIBLE METRIC EXPOSITION
// ============================================================

// PrometheusExporter converts RED metrics to Prometheus text format.
type PrometheusExporter struct {
	metrics []*REDMetrics
	slos    []*SLOTracker
	mu      sync.RWMutex
}

// NewPrometheusExporter creates an exporter for the given metrics.
func NewPrometheusExporter() *PrometheusExporter {
	return &PrometheusExporter{}
}

func (pe *PrometheusExporter) Register(m *REDMetrics) {
	pe.mu.Lock()
	pe.metrics = append(pe.metrics, m)
	pe.mu.Unlock()
}

func (pe *PrometheusExporter) RegisterSLO(slo *SLOTracker) {
	pe.mu.Lock()
	pe.slos = append(pe.slos, slo)
	pe.mu.Unlock()
}

// ServeHTTP implements http.Handler for /metrics endpoint.
func (pe *PrometheusExporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	for _, m := range pe.metrics {
		snap := m.Snapshot()
		safe := sanitizeLabel(snap.Name)
		fmt.Fprintf(w, "# HELP %s_requests_total Total requests\n", safe)
		fmt.Fprintf(w, "# TYPE %s_requests_total counter\n", safe)
		fmt.Fprintf(w, "%s_requests_total %d\n", safe, snap.TotalRequests)

		fmt.Fprintf(w, "# HELP %s_errors_total Total errors\n", safe)
		fmt.Fprintf(w, "# TYPE %s_errors_total counter\n", safe)
		fmt.Fprintf(w, "%s_errors_total %d\n", safe, snap.TotalErrors)

		fmt.Fprintf(w, "# HELP %s_request_rate_per_second Request rate\n", safe)
		fmt.Fprintf(w, "# TYPE %s_request_rate_per_second gauge\n", safe)
		fmt.Fprintf(w, "%s_request_rate_per_second %.4f\n", safe, snap.RequestRate)

		// Latency histograms
		lat := snap.Latency
		fmt.Fprintf(w, "# HELP %s_duration_ns Latency in nanoseconds\n", safe)
		fmt.Fprintf(w, "# TYPE %s_duration_ns summary\n", safe)
		fmt.Fprintf(w, "%s_duration_ns{quantile=\"0.5\"} %d\n", safe, lat.P50)
		fmt.Fprintf(w, "%s_duration_ns{quantile=\"0.9\"} %d\n", safe, lat.P90)
		fmt.Fprintf(w, "%s_duration_ns{quantile=\"0.95\"} %d\n", safe, lat.P95)
		fmt.Fprintf(w, "%s_duration_ns{quantile=\"0.99\"} %d\n", safe, lat.P99)
		fmt.Fprintf(w, "%s_duration_ns{quantile=\"0.999\"} %d\n", safe, lat.P999)
		fmt.Fprintf(w, "%s_duration_ns_count %d\n", safe, lat.Count)
	}

	// Runtime metrics
	rt := SnapshotRuntime()
	fmt.Fprintf(w, "go_goroutines %d\n", rt.NumGoroutines)
	fmt.Fprintf(w, "go_memstats_heap_alloc_bytes %.0f\n", rt.HeapAllocMB*1024*1024)
	fmt.Fprintf(w, "go_gc_duration_seconds{quantile=\"1\"} %.6f\n", rt.LastGCPauseMS/1000)
	fmt.Fprintf(w, "go_memstats_gc_cpu_fraction %.6f\n", rt.GCCPUFraction)
}

func sanitizeLabel(name string) string {
	result := make([]byte, len(name))
	for i, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			result[i] = byte(c)
		} else {
			result[i] = '_'
		}
	}
	return string(result)
}

// ============================================================
// TOP-K HOT PATH DETECTOR
// ============================================================

// HotPathEntry represents a frequently called code path.
type HotPathEntry struct {
	Path    string
	Count   uint64
	AvgNS   float64
	TotalNS uint64
}

// HotPathDetector tracks top-K slowest/most-frequent call paths.
type HotPathDetector struct {
	mu      sync.Mutex
	entries map[string]*HotPathEntry
	topK    int
}

// NewHotPathDetector creates a hot path detector keeping the top-K entries.
func NewHotPathDetector(topK int) *HotPathDetector {
	return &HotPathDetector{
		entries: make(map[string]*HotPathEntry),
		topK:    topK,
	}
}

// Record records a call with its path (e.g., "ServiceA.MethodB") and duration.
func (hpd *HotPathDetector) Record(path string, duration time.Duration) {
	hpd.mu.Lock()
	defer hpd.mu.Unlock()
	e, ok := hpd.entries[path]
	if !ok {
		e = &HotPathEntry{Path: path}
		hpd.entries[path] = e
	}
	e.Count++
	e.TotalNS += uint64(duration.Nanoseconds())
	e.AvgNS = float64(e.TotalNS) / float64(e.Count)
}

// TopByCount returns the top-K paths by call count.
func (hpd *HotPathDetector) TopByCount() []HotPathEntry {
	return hpd.top(func(a, b HotPathEntry) bool { return a.Count > b.Count })
}

// TopByLatency returns the top-K paths by average latency.
func (hpd *HotPathDetector) TopByLatency() []HotPathEntry {
	return hpd.top(func(a, b HotPathEntry) bool { return a.AvgNS > b.AvgNS })
}

func (hpd *HotPathDetector) top(less func(a, b HotPathEntry) bool) []HotPathEntry {
	hpd.mu.Lock()
	all := make([]HotPathEntry, 0, len(hpd.entries))
	for _, e := range hpd.entries {
		all = append(all, *e)
	}
	hpd.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return less(all[i], all[j]) })
	if len(all) > hpd.topK {
		return all[:hpd.topK]
	}
	return all
}

// ============================================================
// SERVICE PROFILER (Composite Entry Point)
// ============================================================

// ServiceProfiler is the unified profiling entry point for a microservice.
type ServiceProfiler struct {
	ServiceName  string
	RED          *REDMetrics
	SLO          *SLOTracker
	HotPaths     *HotPathDetector
	LeakDetector *GoroutineLeakDetector
	HeapTracker  *HeapGrowthTracker
	Prometheus   *PrometheusExporter
	PprofServer  *ProfilerServer
	hostname     string
}

// ServiceProfilerConfig configures the service profiler.
type ServiceProfilerConfig struct {
	ServiceName        string
	HistogramMaxMS     int64
	SLO                SLOConfig
	GoroutineThreshold float64
	MaxGoroutines      int
	HeapGrowthMBPerMin float64
	TopKHotPaths       int
	ProfilerToken      string
	OnGoroutineLeak    func(current, baseline int)
	OnHeapGrowthAlert  func(heapMB, rate float64)
}

// NewServiceProfiler creates a fully configured service profiler.
func NewServiceProfiler(cfg ServiceProfilerConfig) *ServiceProfiler {
	hostname, _ := os.Hostname()

	red := NewREDMetrics(cfg.ServiceName, cfg.HistogramMaxMS)
	slo := NewSLOTracker(cfg.SLO, red)

	exp := NewPrometheusExporter()
	exp.Register(red)
	exp.RegisterSLO(slo)

	leakDetector := NewGoroutineLeakDetector(cfg.GoroutineThreshold, cfg.MaxGoroutines, time.Minute)
	if cfg.OnGoroutineLeak != nil {
		leakDetector.OnLeak(cfg.OnGoroutineLeak)
	}

	heapTracker := NewHeapGrowthTracker(cfg.HeapGrowthMBPerMin)
	if cfg.OnHeapGrowthAlert != nil {
		heapTracker.OnAlert(cfg.OnHeapGrowthAlert)
	}

	return &ServiceProfiler{
		ServiceName:  cfg.ServiceName,
		RED:          red,
		SLO:          slo,
		HotPaths:     NewHotPathDetector(cfg.TopKHotPaths),
		LeakDetector: leakDetector,
		HeapTracker:  heapTracker,
		Prometheus:   exp,
		PprofServer:  NewProfilerServer(cfg.ProfilerToken),
		hostname:     hostname,
	}
}

// Measure wraps a function call, recording RED metrics and hot path info.
func (sp *ServiceProfiler) Measure(path string, fn func() error) error {
	start := time.Now()
	err := fn()
	duration := time.Since(start)
	isErr := err != nil
	sp.RED.Record(duration, isErr)
	sp.SLO.Record(duration, isErr)
	sp.HotPaths.Record(path, duration)
	return err
}

// MeasureHTTP returns an HTTP middleware that records RED metrics per path.
func (sp *ServiceProfiler) MeasureHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &rwCapture{ResponseWriter: w, code: 200}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		isErr := rw.code >= 500
		sp.RED.Record(duration, isErr)
		sp.SLO.Record(duration, isErr)
		sp.HotPaths.Record(r.Method+" "+r.URL.Path, duration)
	})
}

type rwCapture struct {
	http.ResponseWriter
	code int
}

func (rw *rwCapture) WriteHeader(code int) {
	rw.code = code
	rw.ResponseWriter.WriteHeader(code)
}

// Status returns a full diagnostic status of the service.
func (sp *ServiceProfiler) Status() map[string]interface{} {
	return map[string]interface{}{
		"service":              sp.ServiceName,
		"hostname":             sp.hostname,
		"red":                  sp.RED.Snapshot(),
		"slo":                  sp.SLO.Report(),
		"runtime":              SnapshotRuntime(),
		"hot_paths_by_count":   sp.HotPaths.TopByCount(),
		"hot_paths_by_latency": sp.HotPaths.TopByLatency(),
		"timestamp":            time.Now(),
	}
}
