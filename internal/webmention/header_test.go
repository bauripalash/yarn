package webmention

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeader(t *testing.T) {
	assert := assert.New(t)

	test := []string{`<http://alice.host/webmention-endpoint>; rel="webmention"`}

	links := GetHeaderLinks(test)
	assert.Equal(1, len(links))
	assert.Equal(links[0].URL.String(), "http://alice.host/webmention-endpoint")
	assert.Equal(links[0].Params["rel"], []string{"webmention"})
}
