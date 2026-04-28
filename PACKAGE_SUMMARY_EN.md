# LIBPACKAGE Summary (English)

Updated: 2026-04-28

## For Dev Team
- The repository supports per-module installation via Go module paths.
- `package_versions.manifest.json` is the source of truth for version and lifecycle status.
- Module lifecycle statuses are: `stable`, `beta`, `deprecated`.
- Release tags follow `module/path/vX.Y.Z`.

## For PM Team
- `stable` modules are production-ready for product commitments.
- `beta` modules are evolving and may change as roadmap advances.
- `deprecated` modules are not recommended for new projects and should be migrated.
- Full role/use-case inventory is documented in `PACKAGE_SUMMARY_AND_VERSIONS.md`.
