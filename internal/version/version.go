package version

// VERSION is the semantic version, overridden at build time.
var VERSION = "0.1.0"

// REVISION is the git commit, set at build time via -ldflags.
var REVISION = "unknown"
