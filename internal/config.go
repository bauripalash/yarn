package internal

import (
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"git.mills.io/yarnsocial/yarn"
	"git.mills.io/yarnsocial/yarn/types"
	"github.com/gabstv/merger"
	"github.com/goccy/go-yaml"
	log "github.com/sirupsen/logrus"
)

var (
	version              SoftwareConfig
	ErrConfigPathMissing = errors.New("error: config file missing")
)

func init() {
	version = SoftwareConfig{
		Software:    "yarnd",
		Author:      "Yarn.social",
		Copyright:   "Copyright (C) 2021-present Yarn.social",
		License:     "MIT License",
		FullVersion: yarn.FullVersion(),
		Version:     yarn.Version,
		Commit:      yarn.Commit,
	}
}

// Settings contains Pod Settings that can be customised via the Web UI
type Settings struct {
	Name        string `yaml:"pod_name"`
	Logo        string `yaml:"pod_logo"`
	Description string `yaml:"pod_description"`

	MaxTwtLength int `yaml:"max_twt_length"`

	OpenProfiles      bool `yaml:"open_profiles"`
	OpenRegistrations bool `yaml:"open_registrations"`

	WhitelistedDomains []string     `yaml:"whitelisted_domains"`
	Features           FeatureFlags `yaml:"features"`
}

// SoftwareConfig contains the server version information
type SoftwareConfig struct {
	Software string

	FullVersion string
	Version     string
	Commit      string

	Author    string
	License   string
	Copyright string
}

// Config contains the server configuration parameters
type Config struct {
	Version SoftwareConfig

	Debug bool

	Data              string
	Name              string
	Logo              string
	Description       string
	Store             string
	Theme             string
	Lang              string
	BaseURL           string
	AdminUser         string
	AdminName         string
	AdminEmail        string
	FeedSources       []string
	RegisterMessage   string
	CookieSecret      string
	TwtPrompts        []string
	TwtsPerPage       int
	MaxUploadSize     int64
	MaxTwtLength      int
	MaxCacheTTL       time.Duration
	FetchInterval     string
	MaxCacheItems     int
	MsgsPerPage       int
	OpenProfiles      bool
	OpenRegistrations bool
	DisableGzip       bool
	DisableMedia      bool
	DisableFfmpeg     bool
	SessionExpiry     time.Duration
	SessionCacheTTL   time.Duration
	TranscoderTimeout time.Duration

	MagicLinkSecret string

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	MaxFetchLimit int64

	APISessionTime time.Duration
	APISigningKey  string

	baseURL *url.URL

	whitelistedDomains []*regexp.Regexp
	WhitelistedDomains []string

	Features FeatureFlags
}

var _ types.FmtOpts = (*Config)(nil)

func (c *Config) IsLocalURL(url string) bool {
	if NormalizeURL(url) == "" {
		return false
	}
	return strings.HasPrefix(NormalizeURL(url), NormalizeURL(c.BaseURL))
}
func (c *Config) LocalURL() *url.URL                  { return c.baseURL }
func (c *Config) ExternalURL(nick, uri string) string { return URLForExternalProfile(c, nick, uri) }
func (c *Config) UserURL(url string) string           { return UserURL(url) }
func (c *Config) URLForUser(user string) string       { return URLForUser(c.BaseURL, user) }
func (c *Config) URLForTag(tag string) string         { return URLForTag(c.BaseURL, tag) }

// Settings returns a `Settings` struct containing pod settings that can
// then be persisted to disk to override some configuration options.
func (c *Config) Settings() *Settings {
	settings := &Settings{}

	if err := merger.MergeOverwrite(settings, c); err != nil {
		log.WithError(err).Warn("error creating pod settings")
	}

	return settings
}

// WhitelistedDomain returns true if the domain provided is a whiltelisted
// domain as per the configuration
func (c *Config) WhitelistedDomain(domain string) (bool, bool) {
	// Always per mit our own domain
	ourDomain := strings.TrimPrefix(strings.ToLower(c.baseURL.Hostname()), "www.")
	if domain == ourDomain {
		return true, true
	}

	// Check against list of whitelistedDomains (regexes)
	for _, re := range c.whitelistedDomains {
		if re.MatchString(domain) {
			return true, false
		}
	}
	return false, false
}

// RandomTwtPrompt returns a random  Twt Prompt for display by the UI
func (c *Config) RandomTwtPrompt() string {
	n := rand.Int() % len(c.TwtPrompts)
	return c.TwtPrompts[n]
}

// Validate validates the configuration is valid which for the most part
// just ensures that default secrets are actually configured correctly
func (c *Config) Validate() error {
	if c.Debug {
		return nil
	}

	if c.CookieSecret == InvalidConfigValue {
		return fmt.Errorf("error: COOKIE_SECRET is not configured!")
	}

	if c.MagicLinkSecret == InvalidConfigValue {
		return fmt.Errorf("error: MAGICLINK_SECRET is not configured!")
	}

	if c.APISigningKey == InvalidConfigValue {
		return fmt.Errorf("error: API_SIGNING_KEY is not configured!")
	}

	if err := WithWhitelistedDomains(c.WhitelistedDomains)(c); err != nil {
		return fmt.Errorf("error applying whitelisted domains: %w", err)
	}

	return nil
}

func (c *Config) TemplatesFS() fs.FS {
	if c.Theme == "" {
		if c.Debug {
			return os.DirFS("./internal/theme/templates")
		}
		templatesFS, err := fs.Sub(builtinThemeFS, "theme/templates")
		if err != nil {
			log.WithError(err).Fatalf("error loading builtin theme templates")
		}
		return templatesFS
	}

	return os.DirFS(filepath.Join(c.Theme, "templates"))
}

// LoadSettings loads pod settings from the given path
func LoadSettings(path string) (*Settings, error) {
	var settings Settings

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	return &settings, nil
}

// Save saves the pod settings to the given path
func (s *Settings) Save(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	data, err := yaml.MarshalWithOptions(s, yaml.Indent(4))
	if err != nil {
		return err
	}

	if _, err = f.Write(data); err != nil {
		return err
	}

	if err = f.Sync(); err != nil {
		return err
	}

	return f.Close()
}
