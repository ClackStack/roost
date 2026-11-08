//go:build !unix

package extract

// limitMemory is a no-op here: GOMEMLIMIT still applies as a soft limit.
func limitMemory(int64) error { return nil }
