//go:build !(((unix && !android) || (js && wasm) || wasip1) && ((!cgo && !darwin) || osusergo))

package approve

// lookupReadsPasswdOnly is false where os/user asks the operating system, or has no lookup at all.
// That covers cgo builds, darwin builds with or without cgo, and android.
const lookupReadsPasswdOnly = false
