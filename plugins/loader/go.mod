module github.com/driftappdev/libpackage/plugins/loader

go 1.23.0

require (
	github.com/driftappdev/libpackage/plugins/manifest v0.0.0
)

replace (
	github.com/driftappdev/libpackage/plugins/manifest => ./../manifest/
)

