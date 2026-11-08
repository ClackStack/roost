//go:build race

package extract

// The child sets RLIMIT_AS after Go startup; under the race detector the
// runtime alone reserves ~2 GB of address space, so the test sandbox must be
// wider than the production default to leave the parser budget meaningful.
const testWorkerMemoryMB int64 = 8192
