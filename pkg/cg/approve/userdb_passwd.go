//go:build ((unix && !android) || (js && wasm) || wasip1) && ((!cgo && !darwin) || osusergo)

package approve

// lookupReadsPasswdOnly is true where os/user resolves a name by parsing /etc/passwd and nothing
// else, so a user kept in a directory service is never found. The build constraint is the one on
// os/user's lookup_unix.go, the file that does the parsing.
const lookupReadsPasswdOnly = true
