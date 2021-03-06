// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package indieweb

import (
	"net/http"
	"net/url"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html"
	"willnorris.com/go/microformats"
)

const (
	maxWebMentionAttempts = 6
)

type WebMention struct {
	inbox        chan *mention
	outbox       chan *mention
	inboxTicker  *time.Ticker
	outboxTicker *time.Ticker
	Mention      func(source, target *url.URL, sourceData *microformats.Data) error
}

func NewWebMention() *WebMention {
	wm := &WebMention{
		inbox:  make(chan *mention, 100),
		outbox: make(chan *mention, 100),
	}
	wm.inboxTicker = time.NewTicker(5 * time.Second)
	wm.outboxTicker = time.NewTicker(5 * time.Second)
	go func() {
		for range wm.inboxTicker.C {
			wm.processInbox()
		}
	}()
	go func() {
		for range wm.outboxTicker.C {
			wm.processOutbox()
		}
	}()
	return wm
}

type mention struct {
	source   *url.URL
	target   *url.URL
	attempts int
}

func (wm *WebMention) GetTargetEndpoint(target *url.URL) (*url.URL, error) {
	res, err := http.Get(target.String())
	if err != nil {
		log.WithError(err).Error("error getting target endpoint")
		return nil, err
	}
	defer res.Body.Close()

	links := GetHeaderLinks(res.Header["Link"])
	log.Debugf("links: %v", links)
	for _, link := range links {
		for _, rel := range link.Params["rel"] {
			if rel == "webmention" || rel == "http://webmention.org" {
				return link.URL, nil
			}
		}
	}
	log.Debugf("no webmention endpoint found by HTTP Header Link")

	data := microformats.Parse(res.Body, target)

	log.Debugf("Rels: %v", data.Rels["webmention"])
	for _, link := range data.Rels["webmention"] {
		wmurl, err := url.Parse(link)
		if err != nil {
			log.WithError(err).Warn("error parsing webmention link")
			continue
		}
		return wmurl, nil
	}
	log.Debugf("no webmention endpoint found in Document")

	return nil, nil
}

func (wm *WebMention) SendNotification(target *url.URL, source *url.URL) {
	wm.outbox <- &mention{source, target, 0}
}

func (wm *WebMention) WebMentionEndpoint(w http.ResponseWriter, r *http.Request) {
	log.Debug("WebMentionEndpoint:")
	source := r.FormValue("source")
	target := r.FormValue("target")
	log.Debugf("source: %s", source)
	log.Debugf("target: %s", target)
	if source != "" && target != "" {
		sourceurl, err := url.Parse(source)
		if err != nil {
			log.WithError(err).Errorf("error parsing source url: %s", source)
			http.Error(w, "Bad Source URL", http.StatusBadRequest)
			return
		}
		targeturl, err := url.Parse(target)
		if err != nil {
			log.WithError(err).Errorf("error parsing target url: %s", source)
			http.Error(w, "Bad Target URL", http.StatusBadRequest)
			return
		}

		wm.inbox <- &mention{sourceurl, targeturl, 0}

		w.WriteHeader(http.StatusAccepted)
	} else {
		http.Error(w, "Bad Request", http.StatusBadRequest)
	}
}

func (wm *WebMention) processInbox() {
	mention := <-wm.inbox

	log.Debugf("processing mention from %s to %s", mention.source, mention.target)

	res, err := http.Get(mention.source.String())
	if err != nil {
		log.WithError(err).Errorf("error verifying source: %s", mention.source.String())
		return
	}
	defer res.Body.Close()

	if res.StatusCode/100 != 2 {
		log.Errorf("non-200 response %s verifying source: %s", res.Status, mention.source.String())
		return
	}

	node, err := html.Parse(res.Body)
	if err != nil {
		log.Errorf("error parsing source %s: %s", mention.source, err)
		return
	}

	found := searchLinks(node, mention.target)
	if found {
		data := microformats.ParseNode(node, mention.source)
		if err := wm.Mention(mention.source, mention.target, data); err != nil {
			log.WithError(err).Error("error processing webmention")
		}
		return
	}
	log.Debugf("no links found in body, trying headers...")

	links := GetHeaderLinks(res.Header.Values("Link"))
	log.Debugf("links: %v", links)
	if len(links) > 0 {
		if err := wm.Mention(mention.source, mention.target, nil); err != nil {
			log.WithError(err).Error("error processing webmention")
		}
		return
	}
	log.Debugf("no links found in heders, nothing to do!")
}

func (wm *WebMention) processOutbox() {
	mention := <-wm.outbox
	mention.attempts++

	if mention.attempts > maxWebMentionAttempts {
		log.Errorf("giving up processing outgoing webmention for %s after %d attempts", mention.target.String(), mention.attempts)
		return
	}

	endpoint, err := wm.GetTargetEndpoint(mention.target)
	if err != nil {
		log.WithError(err).Errorf("error retrieving webmention endpoint, requeueing (attempts %d)", mention.attempts)
		// Attempt re-delivery
		wm.outbox <- mention
		return
	}
	if endpoint == nil {
		log.Debugf("no endpoint found! requeueing (attempts %d)", mention.attempts)
		return
	}
	values := make(url.Values)
	values.Set("source", mention.source.String())
	values.Set("target", mention.target.String())
	log.Debugf("Sending webmention to %s", endpoint.String())
	log.Debugf("values: %q", values)
	res, err := http.PostForm(endpoint.String(), values)
	if err != nil || (res.StatusCode%100 != 2) {
		log.WithError(err).Errorf(
			"error sending webmention source=%s target=%s status=%s",
			mention.source.String(), mention.target.String(), res.Status,
		)
		// Attempt re-delivery
		wm.outbox <- mention
		return
	}
	defer res.Body.Close()
	log.Debugf(
		"successfully sent webmention to %s (source=%s target=%s)",
		endpoint.String(), mention.source.String(), mention.target.String(),
	)
}
