// Package config loads and validates the Roost TOML configuration.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
	TLSCert        string `toml:"tls_cert"`
	TLSKey         string `toml:"tls_key"`
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
	Exclude []string `toml:"exclude"`
}

// Embeddings configures the optional on-prem embeddings endpoint. It is the
// only feature in Roost that makes outbound connections.
type Embeddings struct {
	Enabled             bool     `toml:"enabled"`
	Endpoint            string   `toml:"endpoint"`
	Model               string   `toml:"model"`
	APIKeyEnv           string   `toml:"api_key_env"`
	BatchSize           int      `toml:"batch_size"`
	Timeout             Duration `toml:"timeout"`
	AllowPublicEndpoint bool     `toml:"allow_public_endpoint"`
}

// Audit configures the access log.
type Audit struct {
	LogQueries bool   `toml:"log_queries"`
	JSONLPath  string `toml:"jsonl_path"`
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

// Validate checks the config and normalizes paths to absolute form.
func (c *Config) Validate() error {
	if len(c.Collections) == 0 {
		return errors.New("config: at least one [[collections]] entry is required")
	}
	seen := map[string]bool{}
	for i := range c.Collections {
		col := &c.Collections[i]
		if !collectionName.MatchString(col.Name) {
			return fmt.Errorf("config: collection name %q must match %s", col.Name, collectionName)
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
	if c.Index.MaxFileMB < 1 || c.Index.MaxTextMB < 1 {
		return errors.New("config: index.max_file_mb and index.max_text_mb must be at least 1")
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
