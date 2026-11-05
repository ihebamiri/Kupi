// Package version holds Kupi's version string.
package version

// Version is overridden at build time with
// -ldflags "-X github.com/iheb-amiri/kupi/internal/version.Version=v0.1.0".
var Version = "0.1.0-dev"

// UserAgent is sent with every request to a Kubernetes API server, so changes
// made by Kupi are easy to find in cluster audit logs.
func UserAgent() string {
	return "kupi/" + Version
}
