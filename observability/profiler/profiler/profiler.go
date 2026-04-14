package profiler

import "net/http"

// Register mounts pprof endpoints onto the provided mux.
func Register(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.Handle("/debug/pprof/", Handler())
}
