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
	"github.com/gabstv/merger"
	"github.com/goccy/go-yaml"
	"github.com/robfig/cron"
	log "github.com/sirupsen/logrus"
	"go.yarn.social/types"
)

var (
	version              SoftwareConfig
	ErrConfigPathMissing = errors.New("error: config file missing")
)

func init() {
	version = SoftwareConfig{
		Software:    "yarnd",
		Author:      "Yarn.social",
		Copyright:   "Copyright (C) 2020-present Yarn.social",
		License:     "AGPLv3 License",
		FullVersion: yarn.FullVersion(),
		Version:     yarn.Version,
		Commit:      yarn.Commit,
	}
}

// Settings contains Pod Settings that can be customised via the Web UI
type Settings struct {
	Name        string `yaml:"pod_name"`
	Logo        string `yaml:"pod_logo"`
	CSS         string `yaml:"pod_css"`
	Description string `yaml:"pod_description"`

	AlertFloat   bool   `yaml:"pod_alert_float"`
	AlertGuest   bool   `yaml:"pod_alert_guest"`
	AlertMessage string `yaml:"pod_alert_message"`
	AlertType    string `yaml:"pod_alert_type"`

	MaxTwtLength     int `yaml:"max_twt_length"`
	TwtsPerPage      int `yaml:"twts_per_page"`
	MediaResolution  int `yaml:"media_resolution"`
	AvatarResolution int `yaml:"avatar_resolution"`

	OpenProfiles      bool `yaml:"open_profiles"`
	OpenRegistrations bool `yaml:"open_registrations"`

	// XXX: Deprecated fields (See: https://git.mills.io/yarnsocial/yarn/pulls/711)
	// TODO: Remove post v0.14.x
	BlacklistedFeeds  []string `yaml:"blacklisted_feeds"`
	WhitelistedImages []string `yaml:"whitelisted_images"`

	BlockedFeeds    []string      `yaml:"blocked_feeds"`
	PermittedImages []string      `yaml:"permitted_images"`
	Features        *FeatureFlags `yaml:"features"`

	CustomDateTime string `yaml:"custom_datetime"`

	// Pod Level Settings (overridable by Users)
	DisplayDatesInTimezone  string `yaml:"display_dates_in_timezone"`
	DisplayTimePreference   string `yaml:"display_time_preference"`
	OpenLinksInPreference   string `yaml:"open_links_in_preference"`
	DisplayImagesPreference string `yaml:"display_images_preference"`
	DisplayMedia            bool   `yaml:"display_media"`
	OriginalMedia           bool   `yaml:"original_media"`

	VisibilityCompact  bool `yaml:"visibility_compact"`
	VisibilityReadmore bool `yaml:"visibility_readmore"`
	LinkVerification   bool `yaml:"link_verification"`
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

	TLS     bool
	TLSKey  string
	TLSCert string

	Data              string `json:"-"`
	Name              string
	Logo              string
	CSS               string
	Description       string
	Store             string `json:"-"`
	Theme             string `json:"-"`
	AlertFloat        bool
	AlertGuest        bool
	AlertMessage      string
	AlertType         string
	Lang              string
	BaseURL           string
	AdminUser         string `json:"-"`
	AdminName         string `json:"-"`
	AdminEmail        string `json:"-"`
	FeedSources       []string
	CookieSecret      string `json:"-"`
	TwtPrompts        []string
	TwtsPerPage       int
	MaxUploadSize     int64
	MaxTwtLength      int
	MediaResolution   int
	AvatarResolution  int
	MaxCacheTTL       time.Duration
	FetchInterval     string
	MaxCacheItems     int
	OpenProfiles      bool
	OpenRegistrations bool
	DisableGzip       bool
	DisableLogger     bool
	DisableMedia      bool
	DisableFfmpeg     bool
	SessionExpiry     time.Duration
	SessionCacheTTL   time.Duration
	TranscoderTimeout time.Duration

	MagicLinkSecret string `json:"-"`

	SMTPHost string `json:"-"`
	SMTPPort int    `json:"-"`
	SMTPUser string `json:"-"`
	SMTPPass string `json:"-"`
	SMTPFrom string `json:"-"`

	MaxCacheFetchers int
	MaxFetchLimit    int64

	APISessionTime time.Duration `json:"-"`
	APISigningKey  string        `json:"-"`

	baseURL *url.URL

	permittedImages []*regexp.Regexp
	PermittedImages []string `json:"-"`

	blockedFeeds []*regexp.Regexp
	BlockedFeeds []string `json:"-"`

	Features *FeatureFlags

	CustomDateTime string

	// Pod Level Settings (overridable by Users)
	DisplayDatesInTimezone  string
	DisplayTimePreference   string
	OpenLinksInPreference   string
	DisplayImagesPreference string
	DisplayMedia            bool
	OriginalMedia           bool

	VisibilityCompact  bool
	VisibilityReadmore bool
	LinkVerification   bool

	// requestTimeout defines the timeout for outgoing HTTP requests.
	requestTimeout time.Duration
}

var _ types.FmtOpts = (*Config)(nil)

func (c *Config) IsLocalURL(url string) bool {
	if NormalizeURL(url) == "" {
		return false
	}
	return strings.HasPrefix(NormalizeURL(url), NormalizeURL(c.BaseURL))
}
func (c *Config) LocalURL() *url.URL                    { return c.baseURL }
func (c *Config) ExternalURL(nick, uri string) string   { return URLForExternalProfile(c, nick, uri) }
func (c *Config) UserURL(url string) string             { return UserURL(url) }
func (c *Config) URLForUser(user string) string         { return URLForUser(c.BaseURL, user) }
func (c *Config) URLForTag(tag string) string           { return URLForTag(c.BaseURL, tag) }
func (c *Config) URLForAvatar(name, hash string) string { return URLForAvatar(c.BaseURL, name, hash) }
func (c *Config) URLForMedia(name string) string        { return URLForMedia(c.BaseURL, name) }

// Settings returns a `Settings` struct containing pod settings that can
// then be persisted to disk to override some configuration options.
func (c *Config) Settings() *Settings {
	settings := &Settings{}

	if err := merger.MergeOverwrite(settings, c); err != nil {
		log.WithError(err).Warn("error creating pod settings")
	}

	return settings
}

// PermittedImage returns true if the domain name of an image's url provided
// is a whiltelisted domain as per the configuration
func (c *Config) PermittedImage(domain string) (bool, bool) {
	// Always permit our own domain
	ourDomain := strings.TrimPrefix(strings.ToLower(c.baseURL.Hostname()), "www.")
	if domain == ourDomain {
		return true, true
	}

	// Check against list of permittedImages (regexes)
	for _, re := range c.permittedImages {
		if re.MatchString(domain) {
			return true, false
		}
	}
	return false, false
}

// BlockedFeed returns true if the feed uri matches any blocked feeds
// per the pod's configuration, the pod itself cannot be blocked.
func (c *Config) BlockedFeed(uri string) bool {
	// Never prohibit the pod itself!
	if strings.HasPrefix(uri, c.BaseURL) {
		return false
	}

	// Check against list of blocked feeds (regexes)
	for _, re := range c.blockedFeeds {
		if re.MatchString(uri) {
			return true
		}
	}
	return false
}

// IsShadowed returns true if a feed has been Shadowed Banned by the Pod Owner/Operator (poderator)
// This is currently functionally equivilent to Blocklisting a feed and uses the same configuration
func (c *Config) IsShadowed(uri string) bool {
	for _, re := range c.blockedFeeds {
		if re.MatchString(uri) {
			return true
		}
	}
	return false
}

// RandomTwtPrompt returns a random  Twt Prompt for display by the UI
func (c *Config) RandomTwtPrompt() string {
	n := rand.Int() % len(c.TwtPrompts)
	return c.TwtPrompts[n]
}

// Validate validates the configuration is valid which for the most part
// just ensures that default secrets are actually configured correctly
func (c *Config) Validate() error {
	//
	// Initlaization
	//

	if err := WithPermittedImages(c.PermittedImages)(c); err != nil {
		return fmt.Errorf("error applying permitted image domains: %w", err)
	}

	if err := WithBlockedFeeds(c.BlockedFeeds)(c); err != nil {
		return fmt.Errorf("error applying blocked feeds: %w", err)
	}

	// Automatically correct missing Scheme in Pod Base URL
	if c.baseURL.Scheme == "" {
		log.Warnf("pod base url (-u/--base-url) %s is missing the scheme", c.BaseURL)
		c.baseURL.Scheme = "http"
		c.BaseURL = c.baseURL.String()
	}

	if c.Debug {
		return nil
	}

	//
	// Validation
	//

	if c.CookieSecret == InvalidConfigValue {
		return fmt.Errorf("error: cookie secret is not configured")
	}

	if c.MagicLinkSecret == InvalidConfigValue {
		return fmt.Errorf("error: magiclink secret is not configured")
	}

	if c.APISigningKey == InvalidConfigValue {
		return fmt.Errorf("error: api signing key is not configured")
	}

	// Automatically correct missing Scheme in Pod Base URL
	if c.baseURL.Scheme == "" {
		log.Warnf("pod base url (-u/--base-url) %s is missing the scheme", c.BaseURL)
		c.baseURL.Scheme = "https"
		c.BaseURL = c.baseURL.String()
	}

	// Validate the Cache Fetch Interval (--fetch-interval)
	schedule, err := cron.Parse(c.FetchInterval)
	if err != nil {
		return fmt.Errorf("error parsing cache fetch interval: %w", err)
	}
	now := time.Now()
	if schedule.Next(now).Sub(now) < MinimumCacheFetchInterval {
		return fmt.Errorf("cache fetch interval cannot be lower than %s for production pods", MinimumCacheFetchInterval)
	}

	// Automatically correct missing AvatarResolution and MediaResolution
	if c.AvatarResolution <= 0 {
		c.AvatarResolution = DefaultAvatarResolution
	}
	if c.MediaResolution <= 0 {
		c.MediaResolution = DefaultMediaResolution
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

// RequestTimeout returns the configured timeout for outgoing HTTP requests. If
// not defined, it defaults to 30 seconds.
func (c *Config) RequestTimeout() time.Duration {
	if c.requestTimeout == 0 {
		return 30 * time.Second
	}
	return c.requestTimeout
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

	if settings.Features == nil {
		settings.Features = NewFeatureFlags()
	}

	// XXX: Deprecated fields (See: https://git.mills.io/yarnsocial/yarn/pulls/711)
	// TODO: Remove post v0.14.x
	if len(settings.BlacklistedFeeds) > 0 && len(settings.BlockedFeeds) == 0 {
		settings.BlockedFeeds = settings.BlacklistedFeeds[:]
		settings.BlacklistedFeeds = nil
	}
	if len(settings.WhitelistedImages) > 0 && len(settings.PermittedImages) == 0 {
		settings.PermittedImages = settings.WhitelistedImages[:]
		settings.WhitelistedImages = nil
	}

	return &settings, nil
}

// Save saves the pod settings to the given path
func (s *Settings) Save(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

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
