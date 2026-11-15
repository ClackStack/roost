// Package config loads and validates the Roost TOML configuration.
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Example is the commented starter configuration, identical to roost.example.toml.
//
//go:embed example.toml
var Example []byte

// WriteExample writes the starter configuration to path, refusing to overwrite
// an existing file.
func WriteExample(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists", path)
		}
		return err
	}
	_, werr := f.Write(Example)
	return errors.Join(werr, f.Close())
}

// Config is the full server configuration.
type Config struct {
	Server      Server       `toml:"server"`
	Storage     Storage      `toml:"storage"`
	Index       Index        `toml:"index"`
	Collections []Collection `toml:"collections"`
	Embeddings  Embeddings   `toml:"embeddings"`
	Audit       Audit        `toml:"audit"`
}

// Server controls the HTTP listener.
type Server struct {
	Listen         string `toml:"listen"`
	TLSCert        string `toml:"tls_cert,omitempty"`
	TLSKey         string `toml:"tls_key,omitempty"`
	AllowPlaintext bool   `toml:"allow_plaintext"`
}

// Storage controls where the index lives.
type Storage struct {
	DataDir string `toml:"data_dir"`
}

// Index controls scanning and extraction.
type Index struct {
	RescanInterval Duration `toml:"rescan_interval"`
	Watch          bool     `toml:"watch"`
	WatchDebounce  Duration `toml:"watch_debounce"`
	Workers        int      `toml:"workers"`
	MaxFileMB      int64    `toml:"max_file_mb"`
	MaxTextMB      int64    `toml:"max_text_mb"`
	ExtractTimeout Duration `toml:"extract_timeout"`
	WorkerMemoryMB int64    `toml:"worker_memory_mb"`
}

// Collection is a named storage path. Collections are the unit of access control.
type Collection struct {
	Name    string   `toml:"name"`
	Path    string   `toml:"path"`
	Exclude []string `toml:"exclude,omitempty"`
}

// Embeddings configures the optional on-prem embeddings endpoint. It is the
// only feature in Roost that makes outbound connections.
type Embeddings struct {
	Enabled             bool     `toml:"enabled"`
	Endpoint            string   `toml:"endpoint,omitempty"`
	Model               string   `toml:"model,omitempty"`
	APIKeyEnv           string   `toml:"api_key_env,omitempty"`
	BatchSize           int      `toml:"batch_size"`
	Timeout             Duration `toml:"timeout"`
	AllowPublicEndpoint bool     `toml:"allow_public_endpoint"`
}

// Audit configures the access log.
type Audit struct {
	LogQueries bool   `toml:"log_queries"`
	JSONLPath  string `toml:"jsonl_path,omitempty"`
}

// Duration parses TOML strings such as "15m".
type Duration struct{ time.Duration }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

// Default returns the configuration used for any value the file leaves unset.
func Default() Config {
	return Config{
		Server:  Server{Listen: "127.0.0.1:8484"},
		Storage: Storage{DataDir: "./data"},
		Index: Index{
			RescanInterval: Duration{15 * time.Minute},
			Watch:          true,
			WatchDebounce:  Duration{5 * time.Second},
			Workers:        max(1, runtime.NumCPU()/2),
			MaxFileMB:      512,
			MaxTextMB:      64,
			ExtractTimeout: Duration{2 * time.Minute},
			WorkerMemoryMB: 4096,
		},
		Embeddings: Embeddings{BatchSize: 32, Timeout: Duration{60 * time.Second}},
		Audit:      Audit{LogQueries: true},
	}
}

// Load reads and validates a config file. Unknown keys are errors, so a typo
// in a security setting cannot silently leave the default in force.
func Load(path string) (*Config, error) {
	cfg := Default()
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("unknown config key %q", undecoded[0].String())
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

var collectionName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidCollectionName reports whether name is valid for a configured collection.
func ValidCollectionName(name string) error {
	if !collectionName.MatchString(name) {
		return fmt.Errorf("config: collection name %q must match %s", name, collectionName)
	}
	return nil
}

// Validate checks the config and normalizes paths to absolute form.
func (c *Config) Validate() error {
	if len(c.Collections) == 0 {
		return errors.New("config: at least one [[collections]] entry is required")
	}
	seen := map[string]bool{}
	for i := range c.Collections {
		col := &c.Collections[i]
		if err := ValidCollectionName(col.Name); err != nil {
			return err
		}
		if seen[col.Name] {
			return fmt.Errorf("config: duplicate collection %q", col.Name)
		}
		seen[col.Name] = true
		if col.Path == "" {
			return fmt.Errorf("config: collection %q has no path", col.Name)
		}
		abs, err := filepath.Abs(col.Path)
		if err != nil {
			return fmt.Errorf("config: collection %q: %w", col.Name, err)
		}
		col.Path = abs
		for _, p := range col.Exclude {
			if _, err := filepath.Match(p, ""); err != nil {
				return fmt.Errorf("config: collection %q: bad exclude pattern %q", col.Name, p)
			}
		}
	}
	if c.Storage.DataDir == "" {
		return errors.New("config: storage.data_dir must not be empty")
	}
	abs, err := filepath.Abs(c.Storage.DataDir)
	if err != nil {
		return fmt.Errorf("config: data_dir: %w", err)
	}
	c.Storage.DataDir = abs

	if (c.Server.TLSCert == "") != (c.Server.TLSKey == "") {
		return errors.New("config: tls_cert and tls_key must be set together")
	}
	if err := c.checkListener(); err != nil {
		return err
	}
	if c.Index.Workers < 1 {
		return errors.New("config: index.workers must be at least 1")
	}
	if c.Index.RescanInterval.Duration < time.Second {
		return errors.New("config: index.rescan_interval must be at least 1s")
	}
	if c.Index.WatchDebounce.Duration < 0 || (c.Index.Watch && c.Index.WatchDebounce.Duration == 0) {
		return errors.New("config: index.watch_debounce must be positive")
	}
	if c.Index.MaxFileMB < 1 || c.Index.MaxTextMB < 1 {
		return errors.New("config: index.max_file_mb and index.max_text_mb must be at least 1")
	}
	if c.Index.ExtractTimeout.Duration <= 0 {
		return errors.New("config: index.extract_timeout must be positive")
	}
	if c.Index.WorkerMemoryMB < 1 {
		return errors.New("config: index.worker_memory_mb must be at least 1")
	}
	if c.Embeddings.Enabled {
		u, err := url.Parse(c.Embeddings.Endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("config: embeddings.endpoint %q must be an http(s) URL", c.Embeddings.Endpoint)
		}
		if c.Embeddings.Model == "" {
			return errors.New("config: embeddings.model is required when embeddings are enabled")
		}
		if c.Embeddings.BatchSize < 1 {
			return errors.New("config: embeddings.batch_size must be at least 1")
		}
		if c.Embeddings.Timeout.Duration <= 0 {
			return errors.New("config: embeddings.timeout must be positive")
		}
	}
	return nil
}

// checkListener refuses to serve documents in cleartext off the local machine
// unless the operator has explicitly accepted that.
func (c *Config) checkListener() error {
	host, _, err := net.SplitHostPort(c.Server.Listen)
	if err != nil {
		return fmt.Errorf("config: server.listen: %w", err)
	}
	if c.Server.TLSCert != "" || c.Server.AllowPlaintext {
		return nil
	}
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("config: server.listen %q is not loopback: set tls_cert and tls_key, or allow_plaintext = true if TLS is terminated in front of Roost", c.Server.Listen)
}

// CollectionNames lists the configured collections in file order.
func (c *Config) CollectionNames() []string {
	names := make([]string, len(c.Collections))
	for i, col := range c.Collections {
		names[i] = col.Name
	}
	return names
}

// Collection finds a configured collection by name.
func (c *Config) Collection(name string) (Collection, bool) {
	for _, col := range c.Collections {
		if col.Name == name {
			return col, true
		}
	}
	return Collection{}, false
}

// Find resolves the config path from a flag, the environment, or the default.
func Find(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("ROOST_CONFIG"); v != "" {
		return v
	}
	return "roost.toml"
}

// Clone returns a deep copy of the configuration.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Collections = make([]Collection, len(c.Collections))
	for i, col := range c.Collections {
		cp.Collections[i] = Collection{
			Name:    col.Name,
			Path:    col.Path,
			Exclude: slices.Clone(col.Exclude),
		}
	}
	return &cp
}

// Render validates and formats cfg as TOML.
func Render(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, errors.New("config: nil configuration")
	}
	cp := cfg.Clone()
	if err := cp.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("# Roost configuration. Unknown keys are errors, so a typo in a\n")
	buf.WriteString("# security setting cannot silently fall back to a default.\n\n")
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(cp); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return buf.Bytes(), nil
}

// Save validates cfg and writes it atomically to path with mode 0600.
func Save(path string, cfg *Config) error {
	data, err := Render(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".roost.toml.*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	committed = true
	return nil
}

// CheckDataDirOverlap rejects configurations where storage.data_dir equals,
// contains, or sits inside any collection path so Roost never indexes its own DB.
func CheckDataDirOverlap(dataDir string, cols []Collection) error {
	if dataDir == "" {
		return nil
	}
	dAbs, dReal := normalizePair(dataDir)
	for _, col := range cols {
		if col.Path == "" {
			continue
		}
		cAbs, cReal := normalizePair(col.Path)
		if pathsOverlap(dAbs, cAbs) || pathsOverlap(dReal, cReal) {
			return fmt.Errorf("collection %q (%s) overlaps storage.data_dir (%s)", col.Name, col.Path, dataDir)
		}
	}
	return nil
}

func normalizePair(p string) (abs, resolved string) {
	a, err := filepath.Abs(p)
	if err != nil {
		a = filepath.Clean(p)
	}
	if r, err := filepath.EvalSymlinks(a); err == nil {
		return a, filepath.Clean(r)
	}
	return a, a
}

func pathsOverlap(a, b string) bool {
	return isSameOrSubpath(a, b) || isSameOrSubpath(b, a)
}

func isSameOrSubpath(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
