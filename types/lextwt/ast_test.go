package lextwt_test

import (
	"testing"

	"git.mills.io/yarnsocial/yarn/types/lextwt"
	"github.com/stretchr/testify/assert"
)

func TestLinkTextToTitle(t *testing.T) {
	for _, tt := range []struct {
		name    string
		link    *lextwt.Link
		expLink *lextwt.Link
		expLit  string
	}{
		{
			name:    "alternative text and title in double quotes",
			link:    lextwt.NewMedia("alt", "https://example.com/", `"title"`),
			expLink: lextwt.NewMedia("alt", "https://example.com/", `"title"`),
			expLit:  `![alt](https://example.com/ "title")`,
		},
		{
			name:    "alternative text and title in single quotes",
			link:    lextwt.NewMedia("alt", "https://example.com/", "'title'"),
			expLink: lextwt.NewMedia("alt", "https://example.com/", "'title'"),
			expLit:  `![alt](https://example.com/ 'title')`,
		},
		{
			name:    "no alternative text and no title",
			link:    lextwt.NewMedia("", "https://example.com/", ""),
			expLink: lextwt.NewMedia("", "https://example.com/", ""),
			expLit:  `![](https://example.com/)`,
		},
		{
			name:    "no alternative text but title",
			link:    lextwt.NewMedia("", "https://example.com/", `"title"`),
			expLink: lextwt.NewMedia("", "https://example.com/", `"title"`),
			expLit:  `![](https://example.com/ "title")`,
		},
		{
			name:    "alternative text without quotes and no title",
			link:    lextwt.NewMedia("alt", "https://example.com/", ""),
			expLink: lextwt.NewMedia("alt", "https://example.com/", `"alt"`),
			expLit:  `![alt](https://example.com/ "alt")`,
		},
		{
			name:    "alternative text with double quotes and no title",
			link:    lextwt.NewMedia(`a"lt`, "https://example.com/", ""),
			expLink: lextwt.NewMedia(`a"lt`, "https://example.com/", `"a\"lt"`),
			expLit:  `![a"lt](https://example.com/ "a\"lt")`,
		},
		{
			name:    "alternative text with single quotes and no title",
			link:    lextwt.NewMedia(`a'lt`, "https://example.com/", ""),
			expLink: lextwt.NewMedia(`a'lt`, "https://example.com/", `"a'lt"`),
			expLit:  `![a'lt](https://example.com/ "a'lt")`,
		},
		{
			name:    "alternative text with double and single quotes and no title",
			link:    lextwt.NewMedia(`a"l't`, "https://example.com/", ""),
			expLink: lextwt.NewMedia(`a"l't`, "https://example.com/", `"a\"l't"`),
			expLit:  `![a"l't](https://example.com/ "a\"l't")`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.link.TextToTitle()
			assert.Equal(t, tt.expLink, tt.link)
			assert.Equal(t, tt.expLit, tt.link.Literal())
		})
	}
}
