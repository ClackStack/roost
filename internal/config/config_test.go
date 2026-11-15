package config

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
		"no collections":          {"", "at least one"},
		"typo key":                {oneCollection + "[server]\nalow_plaintext=true\n", "unknown config key"},
		"bad name":                {"[[collections]]\nname=\"Bad Name\"\npath=\"/x\"\n", "must match"},
		"duplicate":               {oneCollection + oneCollection, "duplicate"},
		"empty data_dir":          {oneCollection + "[storage]\ndata_dir=\"\"\n", "data_dir"},
		"public no tls":           {oneCollection + "[server]\nlisten=\"0.0.0.0:8484\"\n", "not loopback"},
		"cert only":               {oneCollection + "[server]\ntls_cert=\"a\"\n", "set together"},
		"zero watch_debounce":     {oneCollection + "[index]\nwatch=true\nwatch_debounce=\"0s\"\n", "watch_debounce"},
		"negative watch_debounce": {oneCollection + "[index]\nwatch=false\nwatch_debounce=\"-1s\"\n", "watch_debounce"},
		"zero extract_timeout":    {oneCollection + "[index]\nextract_timeout=\"0s\"\n", "extract_timeout"},
		"zero worker_memory":      {oneCollection + "[index]\nworker_memory_mb=0\n", "worker_memory_mb"},
		"embed no model":          {oneCollection + "[embeddings]\nenabled=true\nendpoint=\"http://10.0.0.1/v1\"\n", "model is required"},
		"embed bad url":           {oneCollection + "[embeddings]\nenabled=true\nendpoint=\"nope\"\nmodel=\"m\"\n", "http(s) URL"},
		"embed zero timeout":      {oneCollection + "[embeddings]\nenabled=true\nendpoint=\"http://10.0.0.1/v1\"\nmodel=\"m\"\ntimeout=\"0s\"\n", "embeddings.timeout"},
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

func TestSaveRoundTripAndMode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sub", "roost.toml")
	colPath := filepath.Join(dir, "docs")
	dataPath := filepath.Join(dir, "state")

	want := Default()
	want.Server = Server{
		Listen:         "0.0.0.0:9443",
		AllowPlaintext: true,
	}
	want.Storage = Storage{DataDir: dataPath}
	want.Index.Watch = false
	want.Index.RescanInterval = Duration{30 * time.Minute}
	want.Collections = []Collection{
		{Name: "legal", Path: colPath, Exclude: []string{"*.tmp", "drafts"}},
	}
	want.Audit = Audit{LogQueries: false, JSONLPath: filepath.Join(dir, "audit.jsonl")}
	want.Embeddings = Embeddings{
		Enabled:             true,
		Endpoint:            "http://127.0.0.1:11434/v1/embeddings",
		Model:               "nomic-embed-text",
		APIKeyEnv:           "ROOST_EMBEDDINGS_KEY",
		BatchSize:           16,
		Timeout:             Duration{45 * time.Second},
		AllowPublicEndpoint: false,
	}

	if err := Save(cfgPath, &want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", fi.Mode().Perm())
	}

	got, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got.Server != want.Server {
		t.Errorf("Server = %+v, want %+v", got.Server, want.Server)
	}
	if got.Storage.DataDir != dataPath {
		t.Errorf("DataDir = %q, want %q", got.Storage.DataDir, dataPath)
	}
	if got.Index.Watch || got.Index.RescanInterval.Duration != 30*time.Minute {
		t.Errorf("Index = %+v", got.Index)
	}
	if len(got.Collections) != 1 || got.Collections[0].Name != "legal" || !slices.Equal(got.Collections[0].Exclude, []string{"*.tmp", "drafts"}) {
		t.Errorf("Collections = %+v", got.Collections)
	}
	if got.Audit != want.Audit {
		t.Errorf("Audit = %+v, want %+v", got.Audit, want.Audit)
	}
	if got.Embeddings != want.Embeddings {
		t.Errorf("Embeddings = %+v, want %+v", got.Embeddings, want.Embeddings)
	}

	// Invalid config must fail without overwriting the working file.
	bad := Default()
	if err := Save(cfgPath, &bad); err == nil {
		t.Fatal("expected Save to reject config with no collections")
	}
	if _, err := Load(cfgPath); err != nil {
		t.Fatalf("existing config damaged by failed Save: %v", err)
	}
}

func TestCheckDataDirOverlap(t *testing.T) {
	base := t.TempDir()
	cases := []struct {
		name    string
		dataDir string
		colPath string
		wantErr bool
	}{
		{"identical", filepath.Join(base, "data"), filepath.Join(base, "data"), true},
		{"data inside collection", filepath.Join(base, "docs", "data"), filepath.Join(base, "docs"), true},
		{"collection inside data", filepath.Join(base, "data"), filepath.Join(base, "data", "docs"), true},
		{"sibling prefix", filepath.Join(base, "data"), filepath.Join(base, "data2"), false},
		{"separate siblings", filepath.Join(base, "state"), filepath.Join(base, "docs"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckDataDirOverlap(tc.dataDir, []Collection{{Name: "docs", Path: tc.colPath}})
			if (err != nil) != tc.wantErr {
				t.Fatalf("CheckDataDirOverlap(%q, %q) err = %v, wantErr=%v", tc.dataDir, tc.colPath, err, tc.wantErr)
			}
		})
	}
}
