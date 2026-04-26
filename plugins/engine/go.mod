module github.com/driftappdev/libpackage/plugins/engine

go 1.23.0

require github.com/driftappdev/libpackage/plugins/common v0.0.0

require (
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
)

replace github.com/driftappdev/libpackage/plugins/common => ../common
