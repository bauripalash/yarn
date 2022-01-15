package internal

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"git.mills.io/yarnsocial/yarn/types"
	"github.com/julienschmidt/httprouter"
)

const (
	IPPPubEndpoint = "/ipp/pub"
	IPPSubEndpoint = "/ipp/sub"
)

// IPPStore ...
type IPPStore struct {
	sync.RWMutex
	subscribers map[string]bool
}

// Init ...
func (i *IPPStore) Init() {
	i.Lock()
	defer i.Unlock()
	i.subscribers = make(map[string]bool)
}

// Add adds a subscriber to the store.
func (i *IPPStore) Add(url string) {
	i.Lock()
	defer i.Unlock()
	i.subscribers[url] = true
}

// Remove removes a subscriber from the store.
func (i *IPPStore) Remove(url string) {
	i.Lock()
	defer i.Unlock()
	delete(i.subscribers, url)
}

// Get returns a list of all subscribers.
func (i *IPPStore) Get() map[string]bool {
	i.RLock()
	defer i.RUnlock()
	return i.subscribers
}

// NewIPPStore ...
func NewIPPStore() *IPPStore {
	var store IPPStore
	store.Init()
	return &store
}

// IPPPubHandler handles publish events received from peer pods.
//
// The parameter :url: is passed as a header value, corresponding to
// the feed url that was just updated.
//
// To prevent malicious cache insertions with unwanted feeds, we
// only fetch feeds we have previously cached, from twters we have
// previously cached.
func (s *Server) IPPPubHandler() httprouter.Handle {
	var isLocalUrl = IsLocalURLFactory(s.config)
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		if !s.config.Features.IsEnabled(FeatureIPP) {
			return
		}

		// The URI of the feed that got updated.
		uri := r.Header.Get("x-ipp-uri")

		// Sanity check.
		if uri == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(http.StatusText(http.StatusBadRequest)))
			return
		}

		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(http.StatusText(http.StatusAccepted)))

		// Ignore blacklisted feeds, as well as local feeds.
		if s.cache.conf.BlacklistedFeed(uri) || isLocalUrl(uri) {
			return
		}

		// Only refresh feeds that we have previously cached.
		if !s.cache.IsCached(uri) {
			return
		}

		// Pull the twter from the cache.
		twter := s.cache.GetTwter(uri)
		if twter == nil {
			return
		}

		// Refresh the feed.
		s.tasks.DispatchFunc(func() error {
			sources := make(types.Feeds)
			sources[types.Feed{Nick: twter.Nick, URL: twter.URI}] = true
			s.cache.FetchFeeds(s.config, s.archive, sources, nil)
			return nil
		})
	}
}

// IPPSubHandler handles subscription requests from pods that want
// to subscribe to publish events.
func (s *Server) IPPSubHandler() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		if !s.config.Features.IsEnabled(FeatureIPP) {
			return
		}

		// The other pod's IPP publish endpoint, where we should send
		// publish events.
		callback := r.Header.Get("x-ipp-callback")

		// Validate URL.
		_, err := url.Parse(callback)
		if callback == "" || err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(http.StatusText(http.StatusBadRequest)))
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(http.StatusText(http.StatusOK)))

		s.subscribers.Add(callback)
	}
}

// PublishIPP publishes an IPP notification to all subscribed pods,
// notifying them of an updated feed on this pod.
//
// Publish events are sent concurrently, in order to avoid a slow pod
// causing an upstream latency issue.
func (s *Server) PublishIPP(twter types.Twter) {
	s.tasks.DispatchFunc(func() error {
		var resp *http.Response
		client := http.Client{
			Timeout: 5 * time.Second,
		}

		// Send a publish event to all subscribers.
		for sub := range s.subscribers.Get() {
			go func(sub string) {
				req, _ := http.NewRequest(http.MethodPost, sub, nil)
				req.Header.Set("x-ipp-uri", sub)
				resp, _ = client.Do(req)

				// The receiving pod has received the request but doesn't
				// recognize it, therefore stop sending it publish events.
				//
				// This can happen:
				//	1) If the other pod has IPP disabled
				//	2) If we sent a bad IPP request
				//	3) The receiver wasn't a pod at all
				if resp.StatusCode != http.StatusAccepted {
					s.subscribers.Remove(sub)
				}
				resp.Body.Close()
			}(sub)
		}
		return nil
	})
}

// SubscribeIPP subscribes this pod to another pod's IPP notifications.
func (s *Server) SubscribeIPP(feeds types.Feeds) {
	var isLocalUrl = IsLocalURLFactory(s.config)

	var resp *http.Response
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	// Subscribe to each feed to start receiving publish events from
	// each one.
	for feed := range feeds {
		// Don't subscribe to our own pod.
		if isLocalUrl(feed.URL) {
			continue
		}
		// Validate URL.
		host, err := url.Parse(feed.URL)
		if err != nil {
			continue
		}
		// Make a subscription request.
		req, _ := http.NewRequest(http.MethodPost, host.Host+IPPSubEndpoint, nil)
		req.Header.Set("x-ipp-callback", s.config.LocalURL().Host+IPPPubEndpoint)
		resp, _ = client.Do(req)
		resp.Body.Close()
	}
}
