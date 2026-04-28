module github.com/platformcore/libpackage/messaging/example_integration

go 1.25.0

replace github.com/platformcore/libpackage/messaging/dlq => ../dlq

replace github.com/platformcore/libpackage/messaging/inbox => ../inbox

replace github.com/platformcore/libpackage/messaging/outbox => ../outbox

replace github.com/platformcore/libpackage/messaging/redrive => ../redrive

require (
	github.com/platformcore/libpackage/messaging/dlq v0.0.0-00010101000000-000000000000
	github.com/platformcore/libpackage/messaging/inbox v0.0.0-00010101000000-000000000000
	github.com/platformcore/libpackage/messaging/outbox v0.0.0-00010101000000-000000000000
	github.com/platformcore/libpackage/messaging/redrive v0.0.0-00010101000000-000000000000
)

