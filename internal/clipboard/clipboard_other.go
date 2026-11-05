//go:build !windows

package clipboard

// Native returns the platform clipboard. Outside Windows that is the app's
// own (Wails) clipboard; Linux clipboard managers may still keep history.
func Native(fallback Backend) Backend { return fallback }
