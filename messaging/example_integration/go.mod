module github.com/driftappdev/messaging/example_integration

go 1.25.0

replace github.com/driftappdev/messaging/dlq => ../dlq

replace github.com/driftappdev/messaging/inbox => ../inbox

replace github.com/driftappdev/messaging/outbox => ../outbox

replace github.com/driftappdev/messaging/redrive => ../redrive

require (
	github.com/driftappdev/messaging/dlq v0.0.0-00010101000000-000000000000
	github.com/driftappdev/messaging/inbox v0.0.0-00010101000000-000000000000
	github.com/driftappdev/messaging/outbox v0.0.0-00010101000000-000000000000
	github.com/driftappdev/messaging/redrive v0.0.0-00010101000000-000000000000
)

