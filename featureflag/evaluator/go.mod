module github.com/driftappdev/libpackage/featureflag/evaluator

go 1.23.0

require (
	github.com/driftappdev/libpackage/featureflag/client v0.0.0
	github.com/driftappdev/libpackage/featureflag/types v0.0.0
)

replace (
	github.com/driftappdev/libpackage/featureflag/client => ./../client/
	github.com/driftappdev/libpackage/featureflag/types => ./../types/
)

