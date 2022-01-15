package internal

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"git.mills.io/yarnsocial/yarn/types"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

const (
	IPPPubEndpoint = "/ipp/pub"
	IPPSubEndpoint = "/ipp/sub"
)

// IPPStore ...
type IPPStore struct {
	sync.RWMutex
	subscribers   map[string]bool
	subscriptions map[*Peer]bool
}

// NewIPPStore ...
func NewIPPStore() *IPPStore {
	return &IPPStore{
		subscribers:   make(map[string]bool),
		subscriptions: make(map[*Peer]bool),
	}
}

// AddSubscriber adds a subscriber to the store.
func (i *IPPStore) AddSubscriber(url string) {
	i.Lock()
	defer i.Unlock()
	i.subscribers[url] = true
}

// RemoveSubscriber removes a subscriber from the store.
func (i *IPPStore) RemoveSubscriber(url string) {
	i.Lock()
	defer i.Unlock()
	delete(i.subscribers, url)
}

// GetSubscribers returns a list of all subscribers.
func (i *IPPStore) GetSubscribers() map[string]bool {
	subscribers := make(map[string]bool)

	i.RLock()
	for k, v := range i.subscribers {
		subscribers[k] = v
	}
	i.RUnlock()

	return subscribers
}

// AddSubscription adds a subscription (Pod subscribed to) to the store.
func (i *IPPStore) AddSubscription(peer *Peer) {
	i.Lock()
	defer i.Unlock()
	i.subscriptions[peer] = true
}

// RemoveSubscription removes a subscription (Pod subscribed to) from the store.
func (i *IPPStore) RemoveSubscription(peer *Peer) {
	i.Lock()
	defer i.Unlock()
	delete(i.subscriptions, peer)
}

// GetSubscriptions returns a list of all subscriptions (Peerings pods subscribed to)
func (i *IPPStore) GetSubscriptions() map[*Peer]bool {
	subscriptions := make(map[*Peer]bool)

	i.RLock()
	for k, v := range i.subscriptions {
		subscriptions[k] = v
	}
	i.RUnlock()

	return subscriptions
}

// IsSubscribedTo returns true if this pod is subscribed to the given `pod`.
func (i *IPPStore) IsSubscribedTo(peer *Peer) bool {
	i.RLock()
	defer i.RUnlock()

	if _, found := i.subscriptions[peer]; found {
		return true
	}

	return false
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
			publicFollowers := make(map[types.Feed][]string)
			feed := types.Feed{
				Nick: twter.Nick,
				URL:  twter.URI,
			}
			sources[feed] = true
			users, err := s.db.GetAllUsers()
			if err != nil {
				log.WithError(err).Errorf("error getting all user objects")
				return err
			}
			for _, user := range users {
				if user.IsFollowingPubliclyVisible {
					publicFollowers[feed] = append(publicFollowers[feed], user.Username)
				}
			}
			s.cache.FetchFeeds(s.config, s.archive, sources, publicFollowers)
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

		s.ippStore.AddSubscriber(callback)
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
		for sub := range s.ippStore.GetSubscribers() {
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
					s.ippStore.RemoveSubscriber(sub)
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

func (s *Server) UpdateIPPSubscriptions(user *User) {
	var matchingPeers Peers

	// First get a list of peering Pods
	peeringPods := s.cache.GetPeers()

	// Next get the User's Sources (feeds they follow)
	followingFeeds := user.Sources()

	for followingFeed := range followingFeeds {
		followingURL := NormalizeURL(followingFeed.URL)
		for _, peeringPod := range peeringPods {
			if !peeringPod.IsZero() && strings.HasPrefix(NormalizeURL(peeringPod.URI), followingURL) {
				matchingPeers = append(matchingPeers, peeringPod)
			}
		}
	}

	for _, matchingPeer := range matchingPeers {
		if !s.ippStore.IsSubscribedTo(matchingPeer) {
			s.ippStore.AddSubscription(matchingPeer)
		}
	}
}
