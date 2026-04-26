module github.com/driftappdev/libpackage/persistence

go 1.23.0

require (
	github.com/driftappdev/libpackage/plugins v0.0.0
	github.com/go-redis/redis/v8 v8.11.5
	github.com/google/uuid v1.6.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
)

replace github.com/driftappdev/libpackage/plugins => ../plugins
