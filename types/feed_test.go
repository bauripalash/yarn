package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFeed(t *testing.T) {
	assert := assert.New(t)

	t.Run("String", func(t *testing.T) {
		f := FetchFeedRequest{Nick: "prologic", URL: "https://twtxt.net/user/prologic/twtxt.txt"}
		assert.Equal("FetchFeedRequest: @<prologic https://twtxt.net/user/prologic/twtxt.txt>", f.String())
	})
}
