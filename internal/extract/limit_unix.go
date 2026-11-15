//go:build unix

package extract

import "syscall"

// limitMemory caps the worker's address space, so a decompression bomb ends in
// a failed file rather than the kernel OOM killer choosing a victim. Clamps to
// any lower hard limit already enforced by the host or container.
func limitMemory(bytes int64) error {
	if bytes <= 0 {
		return nil
	}
	want := uint64(bytes)
	var cur syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &cur); err == nil && cur.Max < want {
		want = cur.Max
	}
	lim := syscall.Rlimit{Cur: want, Max: want}
	return syscall.Setrlimit(syscall.RLIMIT_AS, &lim)
}
