module github.com/driftappdev/orchestration/workflow

go 1.25.1

require (
	github.com/go-redis/redis/v8 v8.11.5
	github.com/google/uuid v1.6.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
)

replace github.com/driftappdev/plugins => ../../plugins

