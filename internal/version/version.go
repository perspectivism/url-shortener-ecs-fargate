// Package version exposes build metadata injected at link time.
package version

// value is overridden at build time with
// -ldflags "-X github.com/shortener/url-shortener-ecs-fargate/internal/version.value=<git sha>".
var value = "dev"

// String returns the build version (the git SHA for release builds).
func String() string { return value }
