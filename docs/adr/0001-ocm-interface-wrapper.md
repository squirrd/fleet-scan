# OCM client accessed through an interface, not the SDK directly

The OCM SDK's `Connection` type is concrete and difficult to mock. We wrap it behind an `OCMClient` interface in `internal/ocm/` so that the runner and CLI depend on the interface, not the SDK. This gives us a clean unit-test seam for pagination logic, metadata extraction, and error handling without hitting the real API or faking HTTP endpoints. The trade-off is one layer of indirection, but it's a single file and the interface surface is small (list clusters, get total count).
