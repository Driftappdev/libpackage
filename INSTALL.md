# INSTALL

This workspace now contains two install tracks:

1. Legacy production modules used by existing backend `replace` paths.
2. Namespaced modules under `github.com/driftappdev/libpackage/...`.

Both tracks are listed in:
- `MODULE_CATALOG.md`

## Quick Start

From `lib_package` root:

```bash
go work sync
```

From `lib_package/github.com/driftappdev/libpackage`:

```bash
go work sync
```

## Install Any Module

Use the module path from `MODULE_CATALOG.md`:

```bash
go get <module-path>@latest
```

Examples:

```bash
go get github.com/driftappdev/libpackage/logger@latest
go get github.com/driftappdev/libpackage/middleware/logging@latest
go get github.com/driftappdev/libpackage/eventbus/deadletter@latest
```
