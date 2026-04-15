module github.com/driftappdev/libpackage/featureflag/client

go 1.23.0

require (
	github.com/driftappdev/libpackage/featureflag/types v0.0.0
)

replace (
	github.com/driftappdev/libpackage/featureflag/types => ./../types/
)

