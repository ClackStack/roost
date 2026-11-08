package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// WorkerCommand is the hidden subcommand that runs one extraction.
const WorkerCommand = "extract-worker"

type workerReply struct {
	Result *Result `json:"result,omitempty"`
	Kind   string  `json:"kind,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// Runner extracts files in a child process. Go cannot kill a stuck goroutine,
// and parsers run on hostile input, so a process is the only boundary that
// lets the server survive a hang, crash, or memory blow-up in one file.
type Runner struct {
	Exe      string // path of this binary
	Limits   Limits
	Timeout  time.Duration
	MemoryMB int64
}

// Extract parses f. The child inherits f as stdin and opens nothing itself,
// so it cannot be pointed at another path, even by a swapped symlink. The
// environment is empty so that secrets such as ROOST_TOKEN do not reach it.
func (r *Runner) Extract(ctx context.Context, f *os.File, ext string) (*Result, error) {
	if !Supported(ext) {
		return nil, ErrUnsupported
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.Exe, WorkerCommand,
		"--ext", ext,
		"--size", strconv.FormatInt(fi.Size(), 10),
		"--max-text", strconv.FormatInt(r.Limits.MaxText, 10),
		"--max-unzip", strconv.FormatInt(r.Limits.MaxUnzip, 10),
		"--memory-mb", strconv.FormatInt(r.MemoryMB, 10),
	)
	cmd.Env = []string{"GOMEMLIMIT=" + strconv.FormatInt(r.MemoryMB*3/4, 10) + "MiB", "GOTRACEBACK=none"}
	cmd.Stdin = f
	cmd.WaitDelay = 2 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 2048}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start extractor: %w", err)
	}
	// JSON escaping can expand text, so allow headroom over the text budget.
	maxOut := r.Limits.MaxText*2 + 16<<20
	out, readErr := io.ReadAll(io.LimitReader(stdout, maxOut+1))
	if int64(len(out)) > maxOut {
		cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, fmt.Errorf("extraction timed out after %s", r.Timeout)
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case int64(len(out)) > maxOut:
		return nil, errors.New("extractor output exceeded its limit")
	case waitErr != nil:
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return nil, fmt.Errorf("extractor crashed: %v: %s", waitErr, msg)
	case readErr != nil:
		return nil, readErr
	}
	var reply workerReply
	if err := json.Unmarshal(out, &reply); err != nil {
		return nil, fmt.Errorf("bad extractor output: %w", err)
	}
	switch reply.Kind {
	case "":
		if reply.Result == nil {
			return nil, errors.New("extractor returned nothing")
		}
		return reply.Result, nil
	case "encrypted":
		return nil, ErrEncrypted
	case "unsupported":
		return nil, ErrUnsupported
	}
	return nil, errors.New(reply.Error)
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		b := p[:min(len(p), l.n)]
		l.n -= len(b)
		l.w.Write(b)
	}
	return len(p), nil
}

// WorkerMain is the child entry point. It reads the document from stdin and
// writes one JSON reply to stdout.
func WorkerMain(args []string) int {
	fs := flag.NewFlagSet(WorkerCommand, flag.ContinueOnError)
	ext := fs.String("ext", "", "")
	size := fs.Int64("size", 0, "")
	maxText := fs.Int64("max-text", 0, "")
	maxUnzip := fs.Int64("max-unzip", 0, "")
	memoryMB := fs.Int64("memory-mb", 0, "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := limitMemory(*memoryMB << 20); err != nil {
		fmt.Fprintln(os.Stderr, "set memory limit:", err)
		return 2
	}
	var reply workerReply
	res, err := Extract(os.Stdin, *size, *ext, Limits{MaxText: *maxText, MaxUnzip: *maxUnzip})
	switch {
	case err == nil:
		reply.Result = res
	case errors.Is(err, ErrEncrypted):
		reply.Kind = "encrypted"
	case errors.Is(err, ErrUnsupported):
		reply.Kind = "unsupported"
	default:
		reply.Kind = "error"
		reply.Error = truncateUTF8(clean(err.Error()), 500)
	}
	if err := json.NewEncoder(os.Stdout).Encode(reply); err != nil {
		fmt.Fprintln(os.Stderr, "write reply:", err)
		return 1
	}
	return 0
}
