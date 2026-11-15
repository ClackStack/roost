//go:build unix

package extract

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

const clampHelperEnv = "ROOST_TEST_CLAMP_RLIMIT"

func TestLimitMemoryClampsToExistingHardLimit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLimitMemoryClampsHelper$", "-test.v")
	cmd.Env = append(os.Environ(), clampHelperEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}
}

func TestLimitMemoryClampsHelper(t *testing.T) {
	if os.Getenv(clampHelperEnv) != "1" {
		return
	}
	var orig syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &orig); err != nil {
		t.Fatal(err)
	}
	capBytes := min(orig.Max, uint64(testWorkerMemoryMB+1024)<<20)
	if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: capBytes, Max: capBytes}); err != nil {
		t.Fatal(err)
	}
	// Requesting more than the inherited hard limit must clamp cleanly rather than fail with EPERM.
	wantBytes := int64(capBytes) + (1 << 30)
	r := runner(t)
	r.MemoryMB = wantBytes >> 20
	res, err := r.Extract(context.Background(), tmpFile(t), ".txt")
	if err != nil {
		t.Fatalf("worker extraction failed under capped RLIMIT_AS: %v", err)
	}
	if len(res.Sections) != 1 || !strings.Contains(res.Sections[0].Text, "hi") {
		t.Fatalf("unexpected extraction result: %+v", res)
	}
	if err := limitMemory(wantBytes); err != nil {
		t.Fatalf("limitMemory failed: %v", err)
	}
	var got syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &got); err != nil {
		t.Fatal(err)
	}
	if got.Cur != capBytes || got.Max != capBytes {
		t.Fatalf("rlimit = %+v, want %d", got, capBytes)
	}
}
