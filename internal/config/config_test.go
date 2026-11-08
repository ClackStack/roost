package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, body string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "roost.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

const oneCollection = "[[collections]]\nname=\"legal\"\npath=\"/srv/legal\"\n"

func TestDefaultsAreClosed(t *testing.T) {
	c, err := load(t, oneCollection)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != "127.0.0.1:8484" {
		t.Errorf("listen = %q", c.Server.Listen)
	}
	if c.Embeddings.Enabled {
		t.Error("embeddings must be off by default")
	}
}

func TestRejects(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"no collections": {"", "at least one"},
		"typo key":       {oneCollection + "[server]\nalow_plaintext=true\n", "unknown config key"},
		"bad name":       {"[[collections]]\nname=\"Bad Name\"\npath=\"/x\"\n", "must match"},
		"duplicate":      {oneCollection + oneCollection, "duplicate"},
		"public no tls":  {oneCollection + "[server]\nlisten=\"0.0.0.0:8484\"\n", "not loopback"},
		"cert only":      {oneCollection + "[server]\ntls_cert=\"a\"\n", "set together"},
		"embed no model": {oneCollection + "[embeddings]\nenabled=true\nendpoint=\"http://10.0.0.1/v1\"\n", "model is required"},
		"embed bad url":  {oneCollection + "[embeddings]\nenabled=true\nendpoint=\"nope\"\nmodel=\"m\"\n", "http(s) URL"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := load(t, tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNonLoopbackAllowedWithExplicitChoice(t *testing.T) {
	if _, err := load(t, oneCollection+"[server]\nlisten=\"0.0.0.0:8484\"\nallow_plaintext=true\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := load(t, oneCollection+"[server]\nlisten=\"0.0.0.0:8484\"\ntls_cert=\"c\"\ntls_key=\"k\"\n"); err != nil {
		t.Fatal(err)
	}
}

// TestExampleConfig keeps the shipped example valid, in sync with the embedded
// copy, and with its optional blocks switched on as well as off.
func TestExampleConfig(t *testing.T) {
	b, err := os.ReadFile("../../roost.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, Example) {
		t.Fatal("internal/config/example.toml differs from roost.example.toml")
	}
	if _, err := load(t, string(b)); err != nil {
		t.Fatalf("as shipped: %v", err)
	}
	var enabled []string
	for line := range strings.SplitSeq(string(b), "\n") {
		for _, key := range []string{"[embeddings]", "enabled", "endpoint", "model", "api_key_env", "batch_size", "timeout", "allow_public_endpoint", "tls_cert", "tls_key", "jsonl_path"} {
			if strings.HasPrefix(line, "# "+key) {
				line = strings.TrimPrefix(line, "# ")
			}
		}
		enabled = append(enabled, line)
	}
	c, err := load(t, strings.Join(enabled, "\n"))
	if err != nil {
		t.Fatalf("with options on: %v", err)
	}
	if !c.Embeddings.Enabled || c.Server.TLSCert == "" {
		t.Fatalf("options did not apply: %+v", c)
	}
}

func TestWriteExample(t *testing.T) {
	p := filepath.Join(t.TempDir(), "roost.toml")
	if err := WriteExample(p); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", fi.Mode().Perm())
	}
	if _, err := Load(p); err != nil {
		t.Fatalf("written config is invalid: %v", err)
	}
	if err := WriteExample(p); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite succeeded or wrong error: %v", err)
	}
}
