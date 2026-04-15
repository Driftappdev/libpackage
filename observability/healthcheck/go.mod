module github.com/driftappdev/libpackage/observability/healthcheck

go 1.23.0

require (
	github.com/driftappdev/libpackage/core/types v0.0.0
)

replace (
	github.com/driftappdev/libpackage/core/types => ./../../core/types/
)

