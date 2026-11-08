//go:build unix

package extract

import "syscall"

// limitMemory caps the worker's address space, so a decompression bomb ends in
// a failed file rather than the kernel OOM killer choosing a victim.
func limitMemory(bytes int64) error {
	if bytes <= 0 {
		return nil
	}
	lim := syscall.Rlimit{Cur: uint64(bytes), Max: uint64(bytes)}
	return syscall.Setrlimit(syscall.RLIMIT_AS, &lim)
}
