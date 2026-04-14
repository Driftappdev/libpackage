package main

import (
	"context"
	"log"
	"net/http"

	corecors "github.com/driftappdev/libpackage/middleware/cors"
	corereqid "github.com/driftappdev/libpackage/middleware/requestid"
	corehc "github.com/driftappdev/libpackage/observability/healthcheck"
	corecorr "github.com/driftappdev/libpackage/telemetry/correlation"
	coretrace "github.com/driftappdev/libpackage/telemetry/trace"
)

func main() {
	mux := http.NewServeMux()

	reg := corehc.NewRegistry()
	reg.AddLiveness("self", corehc.FuncChecker(func(context.Context) error { return nil }))
	reg.AddReadiness("self", corehc.FuncChecker(func(context.Context) error { return nil }))

	mux.Handle("/health/live", corehc.LivenessHandler(reg))
	mux.Handle("/health/ready", corehc.ReadinessHandler(reg))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	h := corereqid.Middleware(corecorr.Middleware(corecors.Middleware(corecors.DefaultConfig())(coretrace.HTTPMiddleware("example")(mux))))

	if err := http.ListenAndServe(":8080", h); err != nil {
		log.Fatal(err)
	}
}
