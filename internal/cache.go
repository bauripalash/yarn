// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package internal

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"git.mills.io/yarnsocial/yarn"
	"git.mills.io/yarnsocial/yarn/internal/indieweb"
	"github.com/dustin/go-humanize"
	sync "github.com/sasha-s/go-deadlock"
	log "github.com/sirupsen/logrus"
	"go.yarn.social/types"
)

const (
	feedCacheFile    = "cache"
	feedCacheVersion = 21 // increase this if breaking changes occur to cache file.

	localViewKey    = "local"
	discoverViewKey = "discover"

	podInfoUpdateTTL = time.Hour * 24

	minimumFeedRefresh  = 300.0 // 5m
	maximumFeedRefresh  = 900.0 // 15m
	movingAverageWindow = 7     // no. of most recent twts in moving avg calc
)

var (
	alwaysRefreshDomains = []string{
		"feeds.twtxt.net",
	}
)

// FilterFunc ...
type FilterFunc func(twt types.Twt) bool

// GroupFunc ...
type GroupFunc func(twt types.Twt) []string

// TODO: We need to define a formal way of declaring a feed is automated.
// Suggestion by @jan6 on #yarn.social is to introduce a new field:
// # automated = true
func FilterOutFeedsAndBotsFactory(conf *Config) FilterFunc {
	isLocal := IsLocalURLFactory(conf)
	return func(twt types.Twt) bool {
		twter := twt.Twter()
		if strings.HasPrefix(twter.URI, "https://feeds.twtxt.net") {
			return false
		}
		if strings.HasPrefix(twter.URI, "https://feeds.twtxt.cc") {
			return false
		}
		if strings.HasPrefix(twter.URI, "https://search.twtxt.net") {
			return false
		}
		if isLocal(twter.URI) && HasString(automatedFeeds, twter.Nick) {
			return false
		}
		return true
	}
}

func FilterByMentionFactory(u *User) FilterFunc {
	return func(twt types.Twt) bool {
		for _, mention := range twt.Mentions() {
			if u.Is(mention.Twter().URI) {
				return true
			}
		}
		return false
	}
}

func GroupBySubject(twt types.Twt) []string {
	subject := strings.ToLower(twt.Subject().String())
	if subject == "" {
		return nil
	}
	return []string{subject}
}

func GroupByTag(twt types.Twt) (res []string) {
	var tagsList types.TagList = twt.Tags()
	seenTags := make(map[string]bool)
	for _, tag := range tagsList {
		tagText := strings.ToLower(tag.Text())
		if _, seenTag := seenTags[tagText]; !seenTag {
			res = append(res, tagText)
			seenTags[tagText] = true
		}
	}
	return
}

func FilterTwtsBy(twts types.Twts, f FilterFunc) (res types.Twts) {
	for _, twt := range twts {
		if f(twt) {
			res = append(res, twt)
		}
	}
	return
}

func GroupTwtsBy(twts types.Twts, g GroupFunc) (res map[string]types.Twts) {
	res = make(map[string]types.Twts)
	for _, twt := range twts {
		for _, key := range g(twt) {
			res[key] = append(res[key], twt)
		}
	}
	return
}

func UniqTwts(twts types.Twts) (res types.Twts) {
	seenTwts := make(map[string]bool)
	for _, twt := range twts {
		if _, seenTwt := seenTwts[twt.Hash()]; !seenTwt {
			res = append(res, twt)
			seenTwts[twt.Hash()] = true
		}
	}
	return
}

func ChunkTwts(twts types.Twts, chunkSize int) []types.Twts {
	var chunks []types.Twts
	for i := 0; i < len(twts); i += chunkSize {
		end := i + chunkSize

		// necessary check to avoid slicing beyond
		// slice capacity
		if end > len(twts) {
			end = len(twts)
		}

		chunks = append(chunks, twts[i:end])
	}

	return chunks
}

func FirstTwt(twts types.Twts) types.Twt {
	if len(twts) > 0 {
		return twts[0]
	}
	return types.NilTwt
}

func LastTwt(twts types.Twts) types.Twt {
	if len(twts) > 0 {
		return twts[len(twts)-1]
	}
	return types.NilTwt
}

func FirstNTwts(twts types.Twts, n int) types.Twts {
	if n > len(twts) {
		return twts
	}
	return twts[:n]
}

// Cached ...
type Cached struct {
	mu sync.RWMutex

	Twts          types.Twts
	Errors        int
	LastError     string
	LastFetched   time.Time
	LastModified  string
	MovingAverage float64
}

func NewCached() *Cached {
	return &Cached{}
}

func NewCachedTwts(twts types.Twts, lastModified string) *Cached {
	return &Cached{
		Twts:         twts,
		LastModified: lastModified,
	}
}

// Inject ...
func (cached *Cached) Inject(twt types.Twt) {
	cached.mu.Lock()
	defer cached.mu.Unlock()

	twts := UniqTwts(append(cached.Twts, twt))
	sort.Sort(twts)

	cached.Twts = twts
}

// Snipe deletes a twt from a Cached.
func (cached *Cached) Snipe(twt types.Twt) {
	cached.mu.Lock()
	defer cached.mu.Unlock()

	hash := twt.Hash()
	var twts types.Twts
	for _, t := range cached.Twts {
		if t.Hash() != hash {
			twts = append(twts, t)
		}
	}

	twts = UniqTwts(twts)
	sort.Sort(twts)

	cached.Twts = twts
}

// Update ...
func (cached *Cached) Update(lastmodified string, twts types.Twts) {
	// Avoid overwriting a cached Feed with no Twts
	if len(twts) == 0 {
		return
	}

	cached.mu.Lock()
	defer cached.mu.Unlock()

	oldTwts := cached.Twts[:]

	cached.Twts = twts
	cached.LastModified = lastmodified

	//
	// Calculate the moving average of a feed
	//

	subsetOfTwts := FirstNTwts(append(oldTwts, twts...), movingAverageWindow)

	var deltas []time.Duration
	for i := 0; i < len(subsetOfTwts); i++ {
		if (i + 1) < len(subsetOfTwts) {
			deltas = append(deltas, subsetOfTwts[i].Created().Sub(subsetOfTwts[(i+1)].Created()))
		} else {
			deltas = append(deltas, time.Since(subsetOfTwts[i].Created()))
		}
	}

	var sum float64
	for _, delta := range deltas {
		sum += delta.Seconds()
	}
	avg := sum / float64(len(deltas))

	cached.MovingAverage = (cached.MovingAverage + avg) / 2
}

// GetTwts ...
func (cached *Cached) GetTwts() types.Twts {
	cached.mu.RLock()
	defer cached.mu.RUnlock()

	return cached.Twts
}

// GetLastModified ...
func (cached *Cached) GetLastModified() string {
	cached.mu.RLock()
	defer cached.mu.RUnlock()

	return cached.LastModified
}

// GetLastFetched ...
func (cached *Cached) GetLastFetched() time.Time {
	cached.mu.RLock()
	defer cached.mu.RUnlock()

	return cached.LastFetched
}

// GetMovingAverage ...
func (cached *Cached) GetMovingAverage() float64 {
	cached.mu.RLock()
	defer cached.mu.RUnlock()

	return cached.MovingAverage
}

// UpdateMovingAverage ...
func (cached *Cached) UpdateMovingAverage() {
	cached.mu.Lock()
	defer cached.mu.Unlock()

	if len(cached.Twts) > 0 {
		cached.MovingAverage = (cached.MovingAverage + time.Since(cached.Twts[0].Created()).Seconds()) / 2
	} else {
		if cached.MovingAverage == 0 {
			cached.MovingAverage = maximumFeedRefresh
		}
	}
}

// SetError ...
func (cached *Cached) SetError(err error) {
	cached.mu.Lock()
	defer cached.mu.Unlock()

	cached.Errors++
	cached.LastError = err.Error()
}

// SetLastFetched ...
func (cached *Cached) SetLastFetched() {
	cached.mu.Lock()
	defer cached.mu.Unlock()

	cached.LastFetched = time.Now()
}

type Peer struct {
	URI string `json:"-"`

	Name            string `json:"name"`
	Description     string `json:"description"`
	SoftwareVersion string `json:"software_version"`

	// Maybe we store future data about other peer pods in the future?
	// Right now the above is basically what is exposed now as the pod's name, description and what version of yarnd is running.
	// This information will likely be used for Pod Owner/Operators to manage Permitted Image Domains between pods and internal
	// automated operations like Pod Gossiping of Twts for things like Missing Root Twts for conversation views, etc.

	// lastSeen records the timestamp of when we last saw this pod.
	LastSeen time.Time `json:"-"`

	// lastUpdated is used to periodically re-check the peering pod's /info endpoint in case of changes.
	LastUpdated time.Time `json:"-"`
}

func (p *Peer) String() string {
	return fmt.Sprintf("Peer{Name: %s URI: %s}", p.Name, p.URI)
}

// XXX: Type aliases for backwards compatibility with Cache v19
type PodInfo Peer

func (p *Peer) IsZero() bool {
	return (p == nil) || (p.Name == "" && p.SoftwareVersion == "")
}

func (p *Peer) ShouldRefresh() bool {
	return time.Since(p.LastUpdated) > podInfoUpdateTTL
}

func (p *Peer) makeJsonRequest(conf *Config, path string) ([]byte, error) {
	headers := make(http.Header)
	headers.Set("Accept", "application/json")

	res, err := RequestHTTP(conf, http.MethodGet, p.URI+path, headers)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("non-success HTTP %s response for %s%s", res.Status, p.URI, path)
	}

	if ctype := res.Header.Get("Content-Type"); ctype != "" {
		mediaType, _, err := mime.ParseMediaType(ctype)
		if err != nil {
			return nil, err
		}
		if mediaType != "application/json" {
			return nil, fmt.Errorf("non-JSON response content type '%s' for %s%s", ctype, p.URI, path)
		}
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (p *Peer) GetTwt(conf *Config, hash string) (types.Twt, error) {
	data, err := p.makeJsonRequest(conf, "/twt/"+hash)
	if err != nil {
		return nil, err
	}

	twt, err := types.DecodeJSON(data)
	if err != nil {
		return nil, err
	}

	return twt, nil
}

type Peers []*Peer

func (peers Peers) Len() int           { return len(peers) }
func (peers Peers) Less(i, j int) bool { return strings.Compare(peers[i].Name, peers[j].Name) < 0 }
func (peers Peers) Swap(i, j int)      { peers[i], peers[j] = peers[j], peers[i] }

// Cache ...
type Cache struct {
	mu sync.RWMutex

	conf       *Config
	filterTwts FilterTwtsFunc

	Version int

	List  *Cached
	Map   map[string]types.Twt
	Peers map[string]*Peer
	Feeds map[string]*Cached
	Views map[string]*Cached

	Followers map[string]types.Followers
	Twters    map[string]*types.Twter
}

func (cache *Cache) MarshalJSON() ([]byte, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	return json.Marshal(struct {
		Version int

		List  *Cached
		Map   map[string]types.Twt
		Peers map[string]*Peer
		Feeds map[string]*Cached
		Views map[string]*Cached

		Followers map[string]types.Followers
		Twters    map[string]*types.Twter
	}{
		Version: cache.Version,

		List:  cache.List,
		Map:   cache.Map,
		Peers: cache.Peers,
		Feeds: cache.Feeds,
		Views: cache.Views,

		Followers: cache.Followers,
		Twters:    cache.Twters,
	})
}

func NewCache(conf *Config) *Cache {
	return &Cache{
		conf:       conf,
		filterTwts: FilterTwtsFactory(conf),

		Version: feedCacheVersion,

		List:  NewCached(),
		Map:   make(map[string]types.Twt),
		Peers: make(map[string]*Peer),
		Feeds: make(map[string]*Cached),
		Views: make(map[string]*Cached),

		Followers: make(map[string]types.Followers),
		Twters:    make(map[string]*types.Twter),
	}
}

// FromOldCacheFile attempts to load an oldver version of the on-disk cache stored
// at /path/to/data/cache -- If you change the way the `*Cache` is stored on disk
// by modifying `Cache.Store()` or any of the data structures, please modfy this
// function to support loading the previous version of the on-disk cache.
func FromOldCacheFile(conf *Config, fn string) (*Cache, error) {
	cache := NewCache(conf)

	f, err := os.Open(fn)
	if err != nil {
		if !os.IsNotExist(err) {
			log.WithError(err).Error("error loading cache, cache file found but unreadable")
			return nil, err
		}
		return NewCache(conf), nil
	}
	defer f.Close()

	cleanupCorruptCache := func() (*Cache, error) {
		// Remove invalid cache file.
		os.Remove(fn)
		return NewCache(conf), nil
	}

	dec := gob.NewDecoder(f)

	if err := dec.Decode(&cache.Version); err != nil {
		log.WithError(err).Error("error decoding cache.Version, removing corrupt file")
		return cleanupCorruptCache()
	}

	if err := dec.Decode(&cache.Peers); err != nil {
		log.WithError(err).Error("error decoding cache.Peers, removing corrupt file")
		return cleanupCorruptCache()
	}

	if err := dec.Decode(&cache.Feeds); err != nil {
		log.WithError(err).Error("error decoding cache.Feeds, removing corrupt file")
		return cleanupCorruptCache()
	}

	if err := dec.Decode(&cache.Followers); err != nil {
		log.WithError(err).Warn("error decoding cache.Followers, removing corrupt file")
		return cleanupCorruptCache()
	}

	if err := dec.Decode(&cache.Twters); err != nil {
		log.WithError(err).Warn("error decoding cache.Twters, removing corrupt file")
		return cleanupCorruptCache()
	}

	log.Infof("Loaded old Cache v%d", cache.Version)

	// Migrate old Cache ...

	cache.Version = feedCacheVersion

	// Reset Cache.Twters (let it rebuild correctly)
	cache.Twters = make(map[string]*types.Twter)

	cache.Refresh()

	if err := cache.Store(conf); err != nil {
		log.WithError(err).Errorf("error migrating old cache")
		return cleanupCorruptCache()
	}
	log.Infof("Successfully migrated old cache to v%d", cache.Version)

	return cache, nil
}

// LoadCacheFromFile ...
func LoadCacheFromFile(conf *Config, fn string) (*Cache, error) {
	cache := NewCache(conf)

	f, err := os.Open(fn)
	if err != nil {
		if !os.IsNotExist(err) {
			log.WithError(err).Error("error loading cache, cache file found but unreadable")
			return nil, err
		}
		return NewCache(conf), nil
	}
	defer f.Close()

	dec := gob.NewDecoder(f)

	cleanupCorruptCache := func() (*Cache, error) {
		// Remove invalid cache file.
		os.Remove(fn)
		return NewCache(conf), nil
	}

	if err := dec.Decode(&cache.Version); err != nil {
		log.WithError(err).Error("error decoding cache.Version, removing corrupt file")
		return cleanupCorruptCache()
	}

	if cache.Version != feedCacheVersion {
		log.Warnf(
			"cache.Version %d does not match %d, will try to load old cache v%d instead...",
			cache.Version, feedCacheVersion, (feedCacheVersion - 1),
		)
		cache, err := FromOldCacheFile(conf, fn)
		if err != nil {
			log.WithError(err).Error("error loading old cache, removing corrupt file")
			return cleanupCorruptCache()
		}
		return cache, nil
	}

	if err := dec.Decode(&cache.Peers); err != nil {
		log.WithError(err).Error("error decoding cache.Peers, removing corrupt file")
		return cleanupCorruptCache()
	}

	if err := dec.Decode(&cache.Feeds); err != nil {
		log.WithError(err).Error("error decoding cache.Feeds, removing corrupt file")
		return cleanupCorruptCache()
	}

	if err := dec.Decode(&cache.Followers); err != nil {
		log.WithError(err).Warn("error decoding cache.Followers, removing corrupt file")
		return cleanupCorruptCache()
	}

	if err := dec.Decode(&cache.Twters); err != nil {
		log.WithError(err).Warn("error decoding cache.Twters, removing corrupt file")
		return cleanupCorruptCache()
	}

	log.Infof("Cache version %d", cache.Version)

	return cache, nil
}

// LoadCache ...
func LoadCache(conf *Config) (*Cache, error) {
	fn := filepath.Join(conf.Data, feedCacheFile)
	cache, err := LoadCacheFromFile(conf, fn)
	if err != nil {
		return nil, err
	}
	cache.Refresh()
	return cache, nil
}

// Store ...
func (cache *Cache) Store(conf *Config) error {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	fn := filepath.Join(conf.Data, feedCacheFile)
	f, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.WithError(err).Error("error opening cache file for writing")
		return err
	}
	defer f.Close()

	enc := gob.NewEncoder(f)

	if err := enc.Encode(cache.Version); err != nil {
		log.WithError(err).Error("error encoding cache.Version")
		return err
	}

	if err := enc.Encode(cache.Peers); err != nil {
		log.WithError(err).Error("error encoding cache.Peers")
		return err
	}

	if err := enc.Encode(cache.Feeds); err != nil {
		log.WithError(err).Error("error encoding cache.Feeds")
		return err
	}

	if err := enc.Encode(cache.Followers); err != nil {
		log.WithError(err).Error("error encoding cache.Followers")
		return err
	}

	if err := enc.Encode(cache.Twters); err != nil {
		log.WithError(err).Error("error encoding cache.Twters")
		return err
	}

	return nil
}

func MergeFollowers(old, new types.Followers) types.Followers {
	var res types.Followers

	seen := make(map[string]*types.Follower)

	for _, o := range old {
		// XXX: Backwards compatibility with old `Followers` struct.
		// TODO: Remove post v0.12.x
		if o.URI == "" {
			o.URI = o.URL
		}
		if _, ok := seen[o.URI]; !ok {
			seen[o.URI] = o
			res = append(res, o)
		}
	}

	for _, n := range new {
		if o, ok := seen[n.URI]; ok {
			o.LastSeenAt = n.LastSeenAt
		}

		if _, ok := seen[n.URI]; !ok {
			seen[n.URI] = n
			res = append(res, n)
		}
	}

	return res
}

// DetectClientFromRequest ...
func (cache *Cache) DetectClientFromRequest(req *http.Request, profile types.Profile) error {
	ua, err := ParseUserAgent(req.UserAgent())
	if err != nil {
		return nil
	}

	// Detect Pod (if User-Agent is a pod) and update peering

	if ua.IsPod() {
		if err := cache.DetectPodFromUserAgent(ua); err != nil {
			log.WithError(err).Error("error detecting pod")
			return err
		}
	}

	// Update Followers cache

	newFollowers := ua.Followers(cache.conf)
	currentFollowers := cache.GetFollowers(profile)
	mergedFollowers := MergeFollowers(currentFollowers, newFollowers)

	cache.mu.Lock()
	cache.Followers[profile.Nick] = mergedFollowers
	cache.mu.Unlock()

	return nil
}

// DetectClientFromResponse ...
func (cache *Cache) DetectClientFromResponse(res *http.Response) error {
	poweredBy := res.Header.Get("Powered-By")
	if poweredBy == "" {
		return nil
	}

	ua, err := ParseUserAgent(poweredBy)
	if err != nil {
		log.WithError(err).Warnf("error parsing Powered-By header '%s'", poweredBy)
		return nil
	}

	if cache.conf.Features.IsEnabled(FeatureWebSub) {
		// TOOD: Should this be a function in indieweb package?
		links := indieweb.GetHeaderLinks(res.Header["Link"])
		log.Debugf("links: %v", links)

		var (
			hubEndpoint *url.URL
			selfURL     *url.URL
		)

		for _, link := range links {
			for _, rel := range link.Params["rel"] {
				if rel == "hub" {
					hubEndpoint = link.URL
				}
				if rel == "self" {
					selfURL = link.URL
				}
			}
		}

		if hubEndpoint == nil {
			log.Debugf("no rel=hub link found for %s", res.Request.URL.String())
		} else if selfURL == nil {
			log.Debugf("no rel=self link found for %s", res.Request.URL.String())
		} else if sub := websub.GetSubscription(selfURL.String()); sub != nil {
			log.Debugf("already subscribed to %s", selfURL.String())
		} else {
			callback := fmt.Sprintf("%s/notify", cache.conf.BaseURL)
			if err := websub.Subscribe(selfURL.String(), callback); err != nil {
				log.WithError(err).Errorf("error subscribing to %s", res.Request.URL.RequestURI())
			}
		}
	}

	if err := cache.DetectPodFromUserAgent(ua); err != nil {
		log.WithError(err).Error("error detecting pod")
	}

	return nil
}

// DetectPodFromUserAgent ...
func (cache *Cache) DetectPodFromUserAgent(ua TwtxtUserAgent) error {
	if !ua.IsPod() {
		return nil
	}

	if !cache.conf.Debug && !ua.IsPublicURL() {
		return nil
	}

	podBaseURL := ua.PodBaseURL()
	if podBaseURL == "" {
		return nil
	}

	cache.mu.RLock()
	oldPeer, hasSeen := cache.Peers[podBaseURL]
	cache.mu.RUnlock()

	if hasSeen && !oldPeer.ShouldRefresh() {
		// This might in fact race if another goroutine would have fetched the
		// pod info and updated the cache between our check above and the
		// update here. However, since we're only setting a timestamp when
		// we've last seen the peering pod, this should not be a problem at
		// all. We just override it a fraction of a second later. Doesn't harm
		// anything.
		cache.mu.Lock()
		oldPeer.LastSeen = time.Now()
		cache.mu.Unlock()
		return nil
	}

	// Set an empty &Peer{} to avoid multiple concurrent calls from making
	// multiple callbacks to peering pods unncessarily for Multi-User pods and
	// guard against race from other goroutine doing the same thing.
	cache.mu.Lock()
	oldPeer, hasSeen = cache.Peers[podBaseURL]
	if hasSeen && !oldPeer.ShouldRefresh() {
		cache.mu.Unlock()
		return nil
	}
	cache.Peers[podBaseURL] = &Peer{}
	cache.mu.Unlock()

	resetDummyPeer := func() {
		cache.mu.Lock()
		if oldPeer.IsZero() {
			delete(cache.Peers, podBaseURL)
		} else {
			cache.Peers[podBaseURL] = oldPeer
		}
		cache.mu.Unlock()
	}

	headers := make(http.Header)
	headers.Set("Accept", "application/json")

	res, err := RequestHTTP(cache.conf, http.MethodGet, podBaseURL+"/info", headers)
	if err != nil {
		resetDummyPeer()
		log.WithError(err).Errorf("error making /info request to pod running at %s", podBaseURL)
		return err
	}
	defer res.Body.Close()

	if res.StatusCode/100 != 2 {
		resetDummyPeer()
		log.Errorf("HTTP %s response for /info of pod running at %s", res.Status, podBaseURL)
		return fmt.Errorf("non-success HTTP %s response for %s/info", res.Status, podBaseURL)
	}

	if ctype := res.Header.Get("Content-Type"); ctype != "" {
		mediaType, _, err := mime.ParseMediaType(ctype)
		if err != nil {
			resetDummyPeer()
			log.WithError(err).Errorf("error parsing content type header '%s' for /info of pod running at %s", ctype, podBaseURL)
			return err
		}
		if mediaType != "application/json" {
			resetDummyPeer()
			log.Errorf("non-JSON response '%s' for /info of pod running at %s", ctype, podBaseURL)
			return fmt.Errorf("non-JSON response content type '%s' for %s/info", ctype, podBaseURL)
		}
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		resetDummyPeer()
		log.WithError(err).Errorf("error reading response body for /info of pod running at %s", podBaseURL)
		return err
	}

	var peer Peer

	if err := json.Unmarshal(data, &peer); err != nil {
		resetDummyPeer()
		log.WithError(err).Errorf("error decoding response body for /info of pod running at %s", podBaseURL)
		return err
	}
	peer.URI = podBaseURL
	peer.LastSeen = time.Now()
	peer.LastUpdated = time.Now()

	cache.mu.Lock()
	cache.Peers[podBaseURL] = &peer
	cache.mu.Unlock()

	return nil
}

// FetchFeeds ...
func (cache *Cache) FetchFeeds(conf *Config, archive Archiver, feeds types.FetchFeedRequests, publicFollowers map[types.FetchFeedRequest][]string) {
	stime := time.Now()
	defer func() {
		metrics.Gauge(
			"cache",
			"last_processed_seconds",
		).Set(
			float64(time.Since(stime) / 1e9),
		)
	}()

	isLocalURL := IsLocalURLFactory(conf)

	// buffered to let goroutines write without blocking before the main thread
	// begins reading
	twtsch := make(chan types.Twts, len(feeds))

	var wg sync.WaitGroup
	// max parallel http fetchers
	var fetchers = make(chan struct{}, conf.MaxCacheFetchers)

	seenFeeds := make(map[string]bool)
	for feed := range feeds {
		// Normalize URLs
		feed.URL = NormalizeURL(feed.URL)

		// Skip feeds we've already fetched by URI
		// (but possibly referenced by different alias)
		if _, seenFeed := seenFeeds[feed.URL]; seenFeed {
			continue
		}

		// Skip feeds that are blocked by the Pod
		if cache.conf.BlockedFeed(feed.URL) {
			log.Warnf("attempt to fetch blocked feed %s", feed)
			continue
		}

		wg.Add(1)
		seenFeeds[feed.URL] = true
		fetchers <- struct{}{}

		// anon func takes needed variables as arg, avoiding capture of iterator variables
		go func(feed types.FetchFeedRequest) {
			defer func() {
				<-fetchers
				wg.Done()
			}()

			twter := cache.GetTwter(feed.URL)
			cachedFeed := cache.GetOrSetCachedFeed(feed.URL)

			if twter == nil {
				twter = &types.Twter{
					Nick: feed.Nick,
					URI:  feed.URL,
				}
				if !isLocalURL(feed.URL) {
					GetExternalAvatar(conf, *twter)
				}
			}

			// Handle Feed Refresh
			// Supports three methods of refresh:
			// 1) A refresh interval (suggested refresh interval by feed author), e.g:
			//    # refresh = 1h
			// 2) An exponential back-off based on a weighted moving average of a feed's update frequency (TBD)
			// 3) FetchFeedRequest.Force is `true` so we fetch the feed immediately (Subscription Notification)
			if !feed.Force && !cache.ShouldRefreshFeed(feed.URL) {
				twtsch <- nil
				return
			}

			// Update LastFetched time
			cachedFeed.SetLastFetched()

			// Handle Gopher feeds
			// TODO: Refactor this into some kind of sensible interface
			if strings.HasPrefix(feed.URL, "gopher://") {
				res, err := RequestGopher(conf, feed.URL)
				if err != nil {
					cachedFeed.SetError(err)
					twtsch <- nil
					return
				}

				limitedReader := &io.LimitedReader{R: res.Body, N: conf.MaxFetchLimit}

				tf, err := types.ParseFile(limitedReader, twter)
				if err != nil {
					cachedFeed.SetError(err)
					twtsch <- nil
					return
				}
				if !isLocalURL(twter.Avatar) {
					GetExternalAvatar(conf, *twter)
				}

				future, twts, old := types.SplitTwts(tf.Twts(), conf.MaxCacheTTL, conf.MaxCacheItems)
				if len(future) > 0 {
					log.Warnf("feed %s has %d posts in the future, possible bad client or misconfigured timezone", feed, len(future))
				}

				// If N == 0 we possibly exceeded conf.MaxFetchLimit when
				// reading this feed. Log it and bump a cache_limited counter
				if limitedReader.N <= 0 {
					log.Warnf("feed size possibly exceeds MaxFetchLimit of %s for %s", humanize.Bytes(uint64(conf.MaxFetchLimit)), feed)
					metrics.Counter("cache", "limited").Inc()
				}

				// Archive twts (opportunistically)
				archiveTwts := func(twts []types.Twt) {
					for _, twt := range twts {
						if !archive.Has(twt.Hash()) {
							if err := archive.Archive(twt); err != nil {
								log.WithError(err).Errorf("error archiving twt %s aborting", twt.Hash())
								metrics.Counter("archive", "error").Inc()
							} else {
								metrics.Counter("archive", "size").Inc()
							}
						}
					}
				}
				archiveTwts(old)
				archiveTwts(twts)

				cache.SetTwter(feed.URL, twter)
				cache.UpdateFeed(feed.URL, "", twts)

				twtsch <- twts
				return
			}

			// Handle Gemini feeds
			// TODO: Refactor this into some kind of sensible interface
			if strings.HasPrefix(feed.URL, "gemini://") {
				res, err := RequestGemini(conf, feed.URL)
				if err != nil {
					cachedFeed.SetError(err)
					twtsch <- nil
					return
				}

				limitedReader := &io.LimitedReader{R: res.Body, N: conf.MaxFetchLimit}

				tf, err := types.ParseFile(limitedReader, twter)
				if err != nil {
					cachedFeed.SetError(err)
					twtsch <- nil
					return
				}
				if !isLocalURL(twter.Avatar) {
					GetExternalAvatar(conf, *twter)
				}

				future, twts, old := types.SplitTwts(tf.Twts(), conf.MaxCacheTTL, conf.MaxCacheItems)
				if len(future) > 0 {
					log.Warnf("feed %s has %d posts in the future, possible bad client or misconfigured timezone", feed, len(future))
				}

				// If N == 0 we possibly exceeded conf.MaxFetchLimit when
				// reading this feed. Log it and bump a cache_limited counter
				if limitedReader.N <= 0 {
					log.Warnf("feed size possibly exceeds MaxFetchLimit of %s for %s", humanize.Bytes(uint64(conf.MaxFetchLimit)), feed)
					metrics.Counter("cache", "limited").Inc()
				}

				// Archive twts (opportunistically)
				archiveTwts := func(twts []types.Twt) {
					for _, twt := range twts {
						if !archive.Has(twt.Hash()) {
							if err := archive.Archive(twt); err != nil {
								log.WithError(err).Errorf("error archiving twt %s aborting", twt.Hash())
								metrics.Counter("archive", "error").Inc()
							} else {
								metrics.Counter("archive", "size").Inc()
							}
						}
					}
				}
				archiveTwts(old)
				archiveTwts(twts)

				cache.SetTwter(feed.URL, twter)
				cache.UpdateFeed(feed.URL, "", twts)

				twtsch <- twts
				return
			}

			headers := make(http.Header)

			if publicFollowers != nil {
				feedFollowers := publicFollowers[feed]

				// if no users are publicly following this feed, we rely on the
				// default User-Agent set in the `Request(???)` down below
				if len(feedFollowers) > 0 {
					var userAgent string
					if len(feedFollowers) == 1 {
						userAgent = fmt.Sprintf(
							"yarnd/%s (+%s; @%s)",
							yarn.FullVersion(),
							URLForUser(conf.BaseURL, feedFollowers[0]), feedFollowers[0],
						)
					} else {
						userAgent = fmt.Sprintf(
							"yarnd/%s (~%s; contact=%s)",
							yarn.FullVersion(),
							URLForWhoFollows(conf.BaseURL, feed, len(feedFollowers)),
							URLForPage(conf.BaseURL, "support"),
						)
					}
					headers.Set("User-Agent", userAgent)
				}
			}

			if cachedFeed.GetLastModified() != "" {
				headers.Set("If-Modified-Since", cachedFeed.GetLastModified())
			}

			res, err := RequestHTTP(conf, http.MethodGet, feed.URL, headers)
			if err != nil {
				cachedFeed.SetError(err)
				twtsch <- nil
				return
			}
			defer res.Body.Close()

			actualURL := res.Request.URL.String()
			if actualURL == "" {
				log.WithField("feed", feed).Warnf("%s trying to redirect to an empty url", feed)
				twtsch <- nil
				return
			}

			if actualURL != feed.URL {
				log.WithError(err).Warnf("feed %s has moved to %s", feed, actualURL)
				cache.mu.Lock()
				cache.Feeds[actualURL] = cachedFeed
				cache.mu.Unlock()
				feed.URL = actualURL
			}

			cache.DetectClientFromResponse(res)

			var twts types.Twts

			switch res.StatusCode {
			case http.StatusOK: // 200
				limitedReader := &io.LimitedReader{R: res.Body, N: conf.MaxFetchLimit}

				tf, err := types.ParseFile(limitedReader, twter)
				if err != nil {
					cachedFeed.SetError(err)
					twtsch <- nil
					return
				}
				if !isLocalURL(twter.Avatar) {
					GetExternalAvatar(conf, *twter)
				}

				future, twts, old := types.SplitTwts(tf.Twts(), conf.MaxCacheTTL, conf.MaxCacheItems)
				if len(future) > 0 {
					log.Warnf("feed %s has %d posts in the future, possible bad client or misconfigured timezone", feed, len(future))
				}

				// If N == 0 we possibly exceeded conf.MaxFetchLimit when
				// reading this feed. Log it and bump a cache_limited counter
				if limitedReader.N <= 0 {
					log.Warnf("feed size possibly exceeds MaxFetchLimit of %s for %s", humanize.Bytes(uint64(conf.MaxFetchLimit)), feed)
					metrics.Counter("cache", "limited").Inc()
				}

				// Archive twts (opportunistically)
				archiveTwts := func(twts []types.Twt) {
					for _, twt := range twts {
						if !archive.Has(twt.Hash()) {
							if err := archive.Archive(twt); err != nil {
								log.WithError(err).Errorf("error archiving twt %s aborting", twt.Hash())
								metrics.Counter("archive", "error").Inc()
							} else {
								metrics.Counter("archive", "size").Inc()
							}
						}
					}
				}
				archiveTwts(old)
				archiveTwts(twts)

				lastmodified := res.Header.Get("Last-Modified")
				cache.SetTwter(feed.URL, twter)
				cache.UpdateFeed(feed.URL, lastmodified, twts)
			case http.StatusNotModified: // 304
				twts = cachedFeed.GetTwts()
				cachedFeed.UpdateMovingAverage()
			case 401, 402, 403, 404, 407, 410, 451:
				// These are permanent 4xx errors and considered a dead feed
				cachedFeed.SetError(types.ErrDeadFeed{Reason: res.Status})
			}

			twtsch <- twts
		}(feed)
	}

	// close twts channel when all goroutines are done
	go func() {
		wg.Wait()
		close(twtsch)
	}()

	for range twtsch {
	}

	// Bust and repopulate twts for GetAll()
	cache.Refresh()
}

// Lookup ...
func (cache *Cache) Lookup(hash string) (types.Twt, bool) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	twt, ok := cache.Map[hash]
	if ok {
		return twt, true
	}
	return types.NilTwt, false
}

func (cache *Cache) FeedCount() int {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	return len(cache.Feeds)
}

func (cache *Cache) TwtCount() int {
	cache.mu.RLock()
	cached := cache.List
	cache.mu.RUnlock()

	if cached != nil {
		return len(cached.GetTwts())
	}

	return 0
}

func GetPeersForCached(cached *Cached, peers map[string]*Peer) Peers {
	var matches Peers

	for _, twt := range cached.GetTwts() {
		twterURL := NormalizeURL(twt.Twter().URI)
		for uri, peer := range peers {
			if strings.HasPrefix(twterURL, NormalizeURL(uri)) {
				matches = append(matches, peer)
			}
		}
	}

	return matches
}

func RandomSubsetOfPeers(peers Peers, pct float64) Peers {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
	return peers[:int(math.Ceil(float64(len(peers))*pct))]
}

// Converge ...
func (cache *Cache) Converge(archive Archiver) {
	stime := time.Now()
	defer func() {
		metrics.Gauge(
			"cache",
			"last_convergence_seconds",
		).Set(
			float64(time.Since(stime) / 1e9),
		)
	}()

	// Missing Root Twts
	// Missing Twt Hash -> List of Peer(s)
	missingRootTwts := make(map[string][]*Peer)
	cache.mu.RLock()
	for subject, cached := range cache.Views {
		if !strings.HasPrefix(subject, "subject:") {
			continue
		}

		hash := ExtractHashFromSubject(subject)
		if _, inCache := cache.Map[hash]; inCache || archive.Has(hash) {
			continue
		}

		peers := GetPeersForCached(cached, cache.Peers)
		if len(peers) == 0 {
			peers = RandomSubsetOfPeers(cache.getPeers(), 0.6)
		}
		missingRootTwts[hash] = peers
	}
	cache.mu.RUnlock()

	metrics.Counter("cache", "missing_twts").Add(float64(len(missingRootTwts)))

	for hash, peers := range missingRootTwts {
		var missingTwt types.Twt
		for _, possiblePeer := range peers {
			if !cache.conf.IsLocalURL(possiblePeer.URI) {
				if twt, err := possiblePeer.GetTwt(cache.conf, hash); err == nil {
					missingTwt = twt
					break
				}
			}
		}
		if missingTwt != nil {
			cache.InjectFeed(missingTwt.Twter().URI, missingTwt)
			GetExternalAvatar(cache.conf, missingTwt.Twter())
		}
	}

	cache.Refresh()
}

// Refresh ...
func (cache *Cache) Refresh() {
	var allTwts types.Twts

	cache.mu.RLock()
	for _, cached := range cache.Feeds {
		allTwts = append(allTwts, cached.GetTwts()...)
	}
	cache.mu.RUnlock()

	allTwts = UniqTwts(allTwts)
	sort.Sort(allTwts)

	//
	// Generate some default views...
	//

	var (
		localTwts    types.Twts
		discoverTwts types.Twts
	)

	byHash := make(map[string]types.Twt)
	byTags := make(map[string]types.Twts)
	bySubjects := make(map[string]types.Twts)

	filterOutFeedsAndBots := FilterOutFeedsAndBotsFactory(cache.conf)
	for _, twt := range allTwts {
		byHash[twt.Hash()] = twt

		if !cache.conf.IsShadowed(twt.Twter().URI) {
			// Pod's Local Timeline (alternate Discover view)
			if cache.conf.IsLocalURL(twt.Twter().URI) {
				localTwts = append(localTwts, twt)
			}
			// Pod's Discover Timeline (Primary Discover view)
			if filterOutFeedsAndBots(twt) {
				discoverTwts = append(discoverTwts, twt)
			}
		}

		for _, k := range GroupByTag(twt) {
			byTags[k] = append(byTags[k], twt)
		}

		for _, k := range GroupBySubject(twt) {
			bySubjects[k] = append(bySubjects[k], twt)
		}
	}

	// Insert at the top of all subject views the original Twt (if any)
	// This is mostly to support "forked" conversations
	for k, v := range bySubjects {
		hash := ExtractHashFromSubject(k)
		if twt, ok := byHash[hash]; ok {
			if len(v) > 0 && v[(len(v)-1)].Hash() != twt.Hash() {
				bySubjects[k] = append(bySubjects[k], twt)
			}
		}
	}

	cache.mu.Lock()
	cache.List = NewCachedTwts(allTwts, "")
	cache.Map = byHash
	cache.Views = map[string]*Cached{
		localViewKey:    NewCachedTwts(localTwts, ""),
		discoverViewKey: NewCachedTwts(discoverTwts, ""),
	}
	for k, v := range byTags {
		cache.Views["tag:"+k] = NewCachedTwts(v, "")
	}
	for k, v := range bySubjects {
		cache.Views["subject:"+k] = NewCachedTwts(v, "")
	}

	// Cleanup dead Peers
	for k, peer := range cache.Peers {
		if (peer.LastSeen.Sub(peer.LastUpdated)) > (podInfoUpdateTTL/2) || time.Since(peer.LastUpdated) > podInfoUpdateTTL {
			delete(cache.Peers, k)
		}
	}
	cache.mu.Unlock()
}

// InjectFeed ...
func (cache *Cache) InjectFeed(url string, twt types.Twt) {
	if _, inCache := cache.Lookup(twt.Hash()); inCache {
		return
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	cached, ok := cache.Feeds[url]

	if !ok {
		cache.Feeds[url] = NewCachedTwts(types.Twts{twt}, time.Now().Format(http.TimeFormat))
	} else {
		cached.Inject(twt)
	}

	// Update the Cache directly
	// XXX: This code was directly lifed from Cache.Refresh()
	// but designed to work with just a single Twt.

	// Update Cache.Map (hash -> Twt)
	cache.Map[twt.Hash()] = twt

	// Update Cache.List ([]Twt)
	cache.List.Inject(twt)

	// Update Cache.Views (Local)
	if cache.conf.IsLocalURL(twt.Twter().URI) {
		if cache.Views[localViewKey] == nil {
			cache.Views[localViewKey] = NewCached()
		}
		cache.Views[localViewKey].Inject(twt)
	}

	// Update Cache.Views (Discover)
	if FilterOutFeedsAndBotsFactory(cache.conf)(twt) {
		if cache.Views[discoverViewKey] == nil {
			cache.Views[discoverViewKey] = NewCached()
		}
		cache.Views[discoverViewKey].Inject(twt)
	}

	//
	// Update Cache.Views (tags and subjects)
	//

	tags := GroupByTag(twt)
	subjects := GroupBySubject(twt)

	for _, tag := range tags {
		key := "tag:" + tag
		if _, ok := cache.Views[key]; !ok {
			cache.Views[key] = NewCached()
		}
		cache.Views[key].Inject(twt)
	}
	for _, subject := range subjects {
		key := "subject:" + subject
		if _, ok := cache.Views[key]; !ok {
			cache.Views[key] = NewCached()
		}

		// Insert at the top of all subject views the original Twt (if any)
		// This is mostly to support "forked" conversations
		hash := ExtractHashFromSubject(subject)
		if rootTwt, ok := cache.Map[hash]; ok {
			cache.Views[key].Inject(rootTwt)
		}

		cache.Views[key].Inject(twt)
	}

	// Update cached Twters
	twter := twt.Twter()
	cache.Twters[twter.URI] = &twter
}

// SnipeFeed deletes a twt from a Cache.
func (cache *Cache) SnipeFeed(url string, twt types.Twt) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cached, ok := cache.Feeds[url]
	if ok {
		cached.Snipe(twt)
	}

	// Update Cache.Map (hash -> Twt)
	delete(cache.Map, twt.Hash())

	// Update Cache.List ([]Twt)
	cache.List.Snipe(twt)
}

// ShouldRefreshFeed ...
func (cache *Cache) ShouldRefreshFeed(uri string) bool {
	cache.mu.RLock()
	cachedFeed, isCachedFeed := cache.Feeds[uri]
	cache.mu.RUnlock()

	if !isCachedFeed {
		return true
	}

	// Always refresh feeds that match `alwaysRefreshDomains` list of domains.
	if u, err := url.Parse(uri); err == nil {
		if HasString(alwaysRefreshDomains, u.Hostname()) {
			return true
		}
	}

	// Always refresh feeds on the same pod.
	if cache.conf.IsLocalURL(uri) {
		return true
	}

	twter := cache.GetTwter(uri)
	if twter == nil {
		return true
	}

	refresh := twter.Metadata.Get("refresh")
	if refresh != "" {
		if n, err := strconv.Atoi(refresh); err == nil {
			return int(time.Since(cachedFeed.GetLastFetched()).Seconds()) >= n
		}
	}

	if cache.conf.Features.IsEnabled(FeatureMovingAverageFeedRefresh) {
		movingAverage := cachedFeed.GetMovingAverage()
		boundedMovingAverage := math.Max(minimumFeedRefresh, math.Min(maximumFeedRefresh, movingAverage))
		lastFetched := time.Since(cachedFeed.GetLastFetched())
		log.
			WithField("minimumFeedRefresh", minimumFeedRefresh).
			WithField("maximumFeedRefresh", maximumFeedRefresh).
			WithField("movingAverage", movingAverage).
			WithField("boundedMovingAverage", boundedMovingAverage).
			Infof("Applying moving average refresh for feed %s (Last Fetched: %s)", uri, lastFetched)
		return math.IsNaN(boundedMovingAverage) || lastFetched.Seconds() > boundedMovingAverage
	}

	return true
}

// UpdateFeed ...
func (cache *Cache) UpdateFeed(url, lastmodified string, twts types.Twts) {
	cache.mu.RLock()
	cached, ok := cache.Feeds[url]
	cache.mu.RUnlock()

	if !ok {
		cache.mu.Lock()
		cache.Feeds[url] = NewCachedTwts(twts, lastmodified)
		cache.mu.Unlock()
	} else {
		cached.Update(lastmodified, twts)
	}
}

func (cache *Cache) getFollowersv1(profile types.Profile) types.Followers {
	return cache.Followers[profile.Nick]
}

// GetOldFollowers ...
// XXX: Returns a map[string]string of nick -> url for APIv1 compat
// TODO: Remove when Mobile App is upgraded
func (cache *Cache) GetOldFollowers(profile types.Profile) map[string]string {
	followers := make(map[string]string)

	cache.mu.RLock()
	defer cache.mu.RUnlock()

	for _, follower := range cache.getFollowersv1(profile) {
		followers[follower.Nick] = follower.URI
	}

	return followers
}

// GetFollowers ...
func (cache *Cache) GetFollowers(profile types.Profile) types.Followers {
	cache.mu.RLock()
	followers := cache.Followers[profile.Nick]
	defer cache.mu.RUnlock()

	// XXX: Backwards compatibility with old `Followers` struct.
	// TODO: Remove post v0.12.00
	for _, f := range followers {
		if f.URI == "" {
			f.URI = f.URL
		}
	}

	return followers
}

// GetFollowerByURI ...
func (cache *Cache) GetFollowerByURI(user *User, uri string) *types.Follower {
	if user == nil || user.IsZero() {
		return nil
	}

	cache.mu.RLock()
	followers := cache.Followers[user.Username]
	defer cache.mu.RUnlock()

	// TODO: Optimize this to be an O(1) lookup with a new Cached View?
	for _, f := range followers {
		if f.URI == uri {
			return f
		}
	}

	return nil
}

func (cache *Cache) FollowedBy(user *User, uri string) bool {
	cache.mu.RLock()
	followers := cache.Followers[user.Username]
	cache.mu.RUnlock()

	// Fast Path
	if user.Is(uri) {
		return true
	}

	// TODO: Optimize and cache this as `Cache.FollowersByURI`
	// e.g: map of `Username` to map of `uri` -> `bool`
	followersByURL := make(map[string]bool)
	for _, follower := range followers {
		followersByURL[follower.URI] = true
	}

	return followersByURL[uri]
}

func (cache *Cache) getPeers() (peers Peers) {
	for k, peer := range cache.Peers {
		if k == "" || peer.IsZero() {
			continue
		}
		peers = append(peers, peer)
	}

	sort.Sort(peers)

	return
}

// GetPeers ...
func (cache *Cache) GetPeers() (peers Peers) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	return cache.getPeers()
}

// GetAll ...
func (cache *Cache) GetAll(refresh bool) types.Twts {
	cache.mu.RLock()
	cached := cache.List
	cache.mu.RUnlock()

	if cached != nil && !refresh {
		return cached.GetTwts()
	}

	cache.Refresh()

	cache.mu.RLock()
	cached = cache.List
	cache.mu.RUnlock()

	if cached != nil {
		return cached.GetTwts()
	}
	return nil
}

func (cache *Cache) FilterBy(f FilterFunc) types.Twts {
	return FilterTwtsBy(cache.GetAll(false), f)
}

func (cache *Cache) GroupBy(g GroupFunc) (res map[string]types.Twts) {
	return GroupTwtsBy(cache.GetAll(false), g)
}

// GetMentions ...
func (cache *Cache) GetMentions(u *User, refresh bool) types.Twts {
	key := fmt.Sprintf("mentions:%s", u.Username)

	cache.mu.RLock()
	cached, ok := cache.Views[key]
	cache.mu.RUnlock()

	if ok && !refresh {
		return cached.GetTwts()
	}

	mentions := cache.FilterBy(FilterByMentionFactory(u))
	twts := cache.filterTwts(u, mentions)
	sort.Sort(twts)

	cache.mu.Lock()
	cache.Views[key] = NewCachedTwts(twts, "")
	cache.mu.Unlock()

	return twts
}

// IsCached ...
func (cache *Cache) IsCached(url string) bool {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	_, ok := cache.Feeds[url]
	return ok
}

// GetOrSetCachedFeed ...
func (cache *Cache) GetOrSetCachedFeed(url string) *Cached {
	cache.mu.RLock()
	cached, ok := cache.Feeds[url]
	cache.mu.RUnlock()

	if !ok {
		cached = NewCached()

		cache.mu.Lock()
		cache.Feeds[url] = cached
		cache.mu.Unlock()
	}

	return cached
}

// GetByView ...
func (cache *Cache) GetByView(key string) types.Twts {
	cache.mu.RLock()
	cached, ok := cache.Views[key]
	cache.mu.RUnlock()

	if ok {
		return cached.GetTwts()
	}
	return nil
}

// GetByUser ...
func (cache *Cache) GetByUser(u *User, refresh bool) types.Twts {
	key := fmt.Sprintf("user:%s", u.Username)

	cache.mu.RLock()
	cached, ok := cache.Views[key]
	cache.mu.RUnlock()

	if ok && !refresh {
		return cached.GetTwts()
	}

	var twts types.Twts

	for feed := range u.Sources() {
		twts = append(twts, cache.GetByURL(feed.URL)...)
	}
	twts = cache.filterTwts(u, twts)
	sort.Sort(twts)

	if u.DisplayTimelinePreference == "flat" {
		var yarns types.Yarns
		subjects := GroupTwtsBy(twts, GroupBySubject)
		for _, chain := range subjects {
			sort.Sort(sort.Reverse(chain))
			yarn := types.Yarn{Root: chain[0]}
			if len(chain) > 1 {
				yarn.Twts = chain[1:]
			}
			yarns = append(yarns, yarn)
		}
		sort.Sort(yarns)
		twts = yarns.AsTwts()
	}

	cache.mu.Lock()
	cache.Views[key] = NewCachedTwts(twts, "")
	cache.mu.Unlock()

	return twts
}

// GetByUserView ...
func (cache *Cache) GetByUserView(u *User, view string, refresh bool) types.Twts {
	if u == nil || u.Username == "" {
		// TODO: Cache anonymojs views?
		return cache.filterTwts(nil, cache.GetByView(view))
	}

	key := fmt.Sprintf("%s:%s", u.Username, view)

	cache.mu.RLock()
	cached, ok := cache.Views[key]
	cache.mu.RUnlock()

	if ok && !refresh {
		return cached.GetTwts()
	}

	twts := cache.filterTwts(u, cache.GetByView(view))
	sort.Sort(twts)

	cache.mu.Lock()
	cache.Views[key] = NewCachedTwts(twts, "")
	cache.mu.Unlock()

	return twts
}

// GetByURL ...
func (cache *Cache) GetByURL(url string) types.Twts {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	if cached, ok := cache.Feeds[url]; ok {
		return cached.GetTwts()
	}
	return types.Twts{}
}

// FindTwter locates a valid cached Twter by searching the Cache for previously
// fetched feeds and their Twter(s) using basic string matching
// TODO: Add Fuzzy matching?
func (cache *Cache) FindTwter(s string) (found *types.Twter) {
	// TODO: Optimize this?
	// TODO: Use a fuzzy search?

	cache.mu.Lock()
	defer cache.mu.Unlock()

	var badKeys []string

	for k, twter := range cache.Twters {
		// XXX: Hack to remove bad data from Netbros pod :/
		if k == "" || twter.URI == "" {
			badKeys = append(badKeys, k)
			continue
		}
		if twter != nil && twter.IsZero() {
			badKeys = append(badKeys, k)
			continue
		}

		if strings.EqualFold(s, twter.Nick) || strings.EqualFold(s, twter.DomainNick()) {
			found = twter
			break
		}
	}

	if len(badKeys) > 0 {
		for _, badKey := range badKeys {
			delete(cache.Twters, badKey)
		}
	}

	if found != nil {
		return found
	}
	return &types.Twter{}
}

// GetTwter ...
func (cache *Cache) GetTwter(uri string) *types.Twter {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return cache.Twters[uri]
}

// SetTwter ...
func (cache *Cache) SetTwter(uri string, twter *types.Twter) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.Twters[uri] = twter
}

// DeleteUserViews ...
func (cache *Cache) DeleteUserViews(u *User) {
	cache.mu.Lock()
	for key := range cache.Views {
		if strings.HasPrefix(key, fmt.Sprintf("%s:", u.Username)) {
			delete(cache.Views, key)
		}
		if strings.HasSuffix(key, fmt.Sprintf(":%s", u.Username)) {
			delete(cache.Views, key)
		}
	}
	cache.mu.Unlock()
}

// DeleteFeeds ...
func (cache *Cache) DeleteFeeds(feeds types.FetchFeedRequests) {
	cache.mu.Lock()
	for feed := range feeds {
		delete(cache.Feeds, feed.URL)
	}
	cache.mu.Unlock()
	cache.Refresh()
}

// Reset ...
func (cache *Cache) Reset() {
	cache.mu.Lock()

	cache.Map = make(map[string]types.Twt)
	cache.Peers = make(map[string]*Peer)
	cache.Feeds = make(map[string]*Cached)
	cache.Views = make(map[string]*Cached)

	cache.Followers = make(map[string]types.Followers)
	cache.Twters = make(map[string]*types.Twter)

	cache.mu.Unlock()
}

// PruneFollowers ...
func (cache *Cache) PruneFollowers(olderThan time.Duration) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	for user, followers := range cache.Followers {
		followers.SortBy("LastSeenAt")
		for i, follower := range followers {
			if time.Since(follower.LastSeenAt) < olderThan {
				followers = followers[i:]
				cache.Followers[user] = followers
				break
			}
		}
	}
}
