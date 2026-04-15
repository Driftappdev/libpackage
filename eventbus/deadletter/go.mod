module github.com/driftappdev/libpackage/eventbus/deadletter

go 1.23.0

require (
	github.com/driftappdev/libpackage/eventbus/subscriber v0.0.0
)

replace (
	github.com/driftappdev/libpackage/eventbus/subscriber => ./../subscriber/
)

