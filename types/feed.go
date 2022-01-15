package types

import "fmt"

// FetchFeedRequest is an single request for a twtxt.txt feed with a cannonical Nickname and URL for the feed
// and optinoal request parameters that affect how the Cache fetches the feed.
type FetchFeedRequest struct {
	Nick string
	URL  string

	// Force whether or not to immediately fetch the feed and bypass Cache.ShouldRefreshFeed()
	Force bool
}

// String implements the Stringer interface and returns the Feed represented
// as a twtxt.txt URI in the form @<nick url>
func (f FetchFeedRequest) String() string {
	return fmt.Sprintf("FetchFeedRequest: @<%s %s>", f.Nick, f.URL)
}

// FetchFeedRequests is a mappping of FetchFeedRequest to booleans used to ensure unique feeds
type FetchFeedRequests map[FetchFeedRequest]bool
