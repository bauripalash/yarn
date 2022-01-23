// webmention project webmention.go
package webmention

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"willnorris.com/go/microformats"
)

type WebMention struct {
	inbox       chan mention
	outbox      chan mention
	inboxTimer  *time.Timer
	outboxTimer *time.Timer
	Mention     func(source, target *url.URL, sourceData *microformats.Data) error
}

func New() *WebMention {
	wm := &WebMention{
		inbox:  make(chan mention, 100),
		outbox: make(chan mention, 100),
	}
	wm.inboxTimer = time.NewTimer(5 * time.Second)
	wm.outboxTimer = time.NewTimer(5 * time.Second)
	go func() {
		for range wm.inboxTimer.C {
			wm.processInbox()
		}
	}()
	go func() {
		for range wm.outboxTimer.C {
			wm.processOutbox()
		}
	}()
	return wm
}

type mention struct {
	source *url.URL
	target *url.URL
}

func (wm *WebMention) GetTargetEndpoint(target *url.URL) (*url.URL, error) {
	res, err := http.Get(target.String())
	if err != nil {
		log.WithError(err).Error("error getting target endpoint")
		return nil, err
	}
	defer res.Body.Close()

	links := GetHeaderLinks(res.Header["Link"])
	for _, link := range links {
		for _, rel := range link.Params["rel"] {
			if rel == "webmention" || rel == "http://webmention.org" {
				return link.URL, nil
			}
		}
	}

	data := microformats.Parse(res.Body, target)

	for _, link := range data.Rels["webmention"] {
		wmurl, err := url.Parse(link)
		if err != nil {
			log.WithError(err).Warn("error parsing webmention link")
			continue
		}
		return wmurl, nil
	}

	return nil, nil
}

func (wm *WebMention) SendNotification(target *url.URL, source *url.URL) {
	wm.outbox <- mention{source, target}
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

		wm.inbox <- mention{
			sourceurl,
			targeturl,
		}

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

	endpoint, err := wm.GetTargetEndpoint(mention.target)
	if err != nil {
		log.WithError(err).Error("error retrieving webmention endpoint")
		return
	}
	if endpoint == nil {
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
		return
	}
	defer res.Body.Close()
	log.Debugf(
		"successfully sent webmention to %s (source=%s target=%s)",
		endpoint.String(), mention.source.String(), mention.target.String(),
	)
}

func searchLinks(node *html.Node, link *url.URL) bool {
	if node.Type == html.ElementNode {
		if node.DataAtom == atom.A {
			if href := getAttr(node, "href"); href != "" {
				target, err := url.Parse(href)
				if err == nil {
					// pods have the form
					// http://pod.domain.tld/external?uri=uri&nick=nick
					if strings.HasPrefix(target.Path, "/external") && target.Query().Get("uri") == link.String() {
						return true
					}
					if target.String() == link.String() {
						return true
					}
				}
			}
		}
	}
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		found := searchLinks(c, link)
		if found {
			return found
		}
	}
	return false
}

func getAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}
