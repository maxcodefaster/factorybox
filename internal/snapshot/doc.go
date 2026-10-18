// Package snapshot reads the pinned CRIU pagemap format and verifies complete
// logical memory images, including inherited pages and image-PID aliases.
//
// The native graph adapter currently retains the private /experiment worker
// protocol. It is an extracted integrity component, not a generic lifecycle API.
package snapshot
