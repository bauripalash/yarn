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

// Users is a list of users that subscribe to a Peer.
type Users []string

// Add ...
func (u *Users) Add(username string) {
	// Avoid duplications.
	for _, user := range *u {
		if user == username {
			return
		}
	}
	users := append(*u, username)
	u = &users
}

// Remove ...
func (u *Users) Remove(username string) {
	var filtered Users
	for _, user := range *u {
		if user != username {
			filtered.Add(user)
		}
	}
	u = &filtered
}

// IPPStore ...
type IPPStore struct {
	sync.RWMutex
	subscribers   map[string]bool
	subscriptions map[*Peer]Users
}

// NewIPPStore ...
func NewIPPStore() *IPPStore {
	return &IPPStore{
		subscribers:   make(map[string]bool),
		subscriptions: make(map[*Peer]Users),
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

// AddSubscription adds a user to a peer subscription in the store.
func (i *IPPStore) AddSubscription(peer *Peer, username string) {
	i.Lock()
	defer i.Unlock()

	// Add the user to the subscriber list.
	list := i.subscriptions[peer]
	list.Add(username)
	i.subscriptions[peer] = list
}

// RemoveSubscription removes a user from a peer subscription in the store.
func (i *IPPStore) RemoveSubscription(peer *Peer, username string) {
	i.Lock()
	defer i.Unlock()

	// Remove the user from the subscriber list.
	list := i.subscriptions[peer]
	list.Remove(username)
	i.subscriptions[peer] = list

	// If there are no longer any subscribers, unsubscribe.
	if len(list) == 0 {
		delete(i.subscriptions, peer)
	}
}

// GetSubscriptions returns a list of all subscriptions (Peerings pods subscribed to)
func (i *IPPStore) GetSubscriptions() map[*Peer]Users {
	subscriptions := make(map[*Peer]Users)

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

// GetPeerSubscribers returns the amount of users subscribed to a peer.
func (i *IPPStore) GetPeerSubscribers(peer *Peer) int {
	i.RLock()
	defer i.RUnlock()

	users, ok := i.subscriptions[peer]
	if !ok {
		return 0
	}

	return len(users)
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

		// Send acceptance to valid peers, and withold acceptance
		// from unsubscribed peers.
		//
		// Without acceptance, the pod that published this event will
		// remove this pod from his list of subscribers, effectively
		// unsubscribing us.
		for peer := range s.ippStore.subscriptions {
			if strings.HasPrefix(uri, NormalizeURL(peer.URI)) {
				w.WriteHeader(http.StatusAccepted)
				w.Write([]byte(http.StatusText(http.StatusAccepted)))
				break
			}
		}

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
			sources := make(types.FetchFeedRequests)
			publicFollowers := make(map[types.FetchFeedRequest][]string)
			feed := types.FetchFeedRequest{
				Nick:  twter.Nick,
				URL:   twter.URI,
				Force: true,
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
func (s *Server) PublishIPP(user *User) {
	s.tasks.DispatchFunc(func() error {
		var resp *http.Response
		client := http.Client{
			Timeout: 5 * time.Second,
		}

		uri := URLForUser(s.config.BaseURL, user.Username)

		// Send a publish event to all subscribers.
		for sub := range s.ippStore.GetSubscribers() {
			go func(sub, uri string) {
				req, _ := http.NewRequest(http.MethodPost, sub, nil)
				req.Header.Set("x-ipp-uri", uri)
				resp, _ = client.Do(req)

				// The receiving pod has received the request but doesn't
				// recognize it, therefore stop sending it publish events.
				//
				// This can happen:
				//	1) If the other pod has IPP disabled
				//  2) The other pod isn't subscribed to us (anymore)
				//	3) If we sent a bad IPP request
				//	4) The receiver wasn't a pod at all
				if resp.StatusCode != http.StatusAccepted {
					s.ippStore.RemoveSubscriber(sub)
				}
				resp.Body.Close()
			}(sub, uri)
		}
		return nil
	})
}

// SubscribeIPP subscribes this pod to another pod's IPP notifications.
func (s *Server) SubscribeIPP(peer *Peer) {
	var resp *http.Response
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	// Make a subscription request to the peer.
	req, _ := http.NewRequest(http.MethodPost, peer.URI+IPPSubEndpoint, nil)
	req.Header.Set("x-ipp-callback", s.config.BaseURL+IPPPubEndpoint)
	resp, _ = client.Do(req)
	resp.Body.Close()
}

// UpdateIPPSubscriptions updates the IPPStore regarding a User's
// followed feeds.
func (s *Server) UpdateIPPSubscriptions(user *User) {
	log.Debugf("Updating subscriptions for %s", user)

	var matchingPeers Peers
	var otherPeers Peers

	// First get a list of peering Pods
	peeringPods := s.cache.GetPeers()

	// Next get the User's Sources (feeds they follow)
	followingFeeds := user.Sources()

	// Split peered pods into pods that this user subscribes to (matchingPeers),
	// and peers that this user doesn't subscribe to (otherPeers).
	for followingFeed := range followingFeeds {
		followingURL := NormalizeURL(followingFeed.URL)
		for _, peeringPod := range peeringPods {
			if !peeringPod.IsZero() && strings.HasPrefix(followingURL, NormalizeURL(peeringPod.URI)) {
				matchingPeers = append(matchingPeers, peeringPod)
			} else {
				otherPeers = append(otherPeers, peeringPod)
			}
		}
	}
	log.Debugf("matchingPeers: %q", matchingPeers)
	log.Debugf("othersPeers: %q", otherPeers)

	// Ensure subscription to followed peers.
	for _, matchingPeer := range matchingPeers {
		s.ippStore.AddSubscription(matchingPeer, user.Username)
		// If we haven't subscribed to this peer in the past, send a
		// subscription request.
		if s.ippStore.GetPeerSubscribers(matchingPeer) == 1 {
			go s.SubscribeIPP(matchingPeer)
		}
	}

	// Ensure unsubscription from unfollowed peers.
	for _, otherPeer := range matchingPeers {
		s.ippStore.RemoveSubscription(otherPeer, user.Username)
	}
}
