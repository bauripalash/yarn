package lextwt_test

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"git.mills.io/yarnsocial/yarn/types"
	"git.mills.io/yarnsocial/yarn/types/lextwt"
	"github.com/stretchr/testify/assert"
)

func TestParseDateTime(t *testing.T) {
	assert := assert.New(t)

	tests := []dateTestCase{
		{lit: "2016-02-03T23:05:00Z", dt: time.Date(2016, 2, 3, 23, 5, 0, 0, time.UTC)},
		{lit: "2016-02-03T23:05:00-0700", dt: time.Date(2016, 2, 3, 23, 5, 0, 0, time.FixedZone("UTC-0700", -7*3600+0*60))},
		{lit: "2016-02-03T23:05:00.000001234+08:45", dt: time.Date(2016, 2, 3, 23, 5, 0, 1234, time.FixedZone("UTC+0845", 8*3600+45*60))},
		{lit: "2016-02-03T23:05", dt: time.Date(2016, 2, 3, 23, 5, 0, 0, time.UTC)},
		{lit: "2016-02-03", errs: []error{lextwt.ErrParseToken}},
		{lit: "2016", errs: []error{lextwt.ErrParseToken}},
		{lit: "22021-02-03", errs: []error{lextwt.ErrParseToken}},
		{lit: "2021-102-03", errs: []error{lextwt.ErrParseToken}},
	}
	for _, tt := range tests {
		r := strings.NewReader(tt.lit)
		lexer := lextwt.NewLexer(r)
		parser := lextwt.NewParser(lexer)
		dt := parser.ParseDateTime()

		if tt.errs == nil {
			assert.True(dt != nil)
			assert.Equal(tt.lit, dt.Literal()) // src value == parsed value
			assert.Equal(tt.dt, dt.DateTime()) // src value == parsed value
		} else {
			assert.True(dt == nil)
			for i, e := range parser.Errs() {
				assert.True(errors.Is(e, tt.errs[i]))
			}
		}
	}
}

type mentionTestCase struct {
	lit    string
	elem   *lextwt.Mention
	errs   []error
	notEOF bool
}

func TestParseMention(t *testing.T) {
	assert := assert.New(t)

	tests := []mentionTestCase{
		{
			lit:  "@<xuu https://sour.is/xuu/twtxt.txt>",
			elem: lextwt.NewMention("xuu", "https://sour.is/xuu/twtxt.txt"),
		},
		{
			lit:  "@<xuu@sour.is https://sour.is/xuu/twtxt.txt>",
			elem: lextwt.NewMention("xuu@sour.is", "https://sour.is/xuu/twtxt.txt"),
		},
		{
			lit:  "@<https://sour.is/xuu/twtxt.txt>",
			elem: lextwt.NewMention("", "https://sour.is/xuu/twtxt.txt"),
		},
		{
			lit:  "@xuu",
			elem: lextwt.NewMention("xuu", ""),
		},
		{
			lit:  "@xuu@sour.is",
			elem: lextwt.NewMention("xuu@sour.is", ""),
		},
		{
			lit:    "@xuu@sour.is. ",
			elem:   lextwt.NewMention("xuu@sour.is", ""),
			notEOF: true,
		},
	}

	for _, tt := range tests[5:] {
		r := strings.NewReader(tt.lit)
		lexer := lextwt.NewLexer(r)
		parser := lextwt.NewParser(lexer)
		elem := parser.ParseMention()

		if !tt.notEOF {
			assert.True(parser.IsEOF())
		}

		if len(tt.errs) == 0 {
			testParseMention(t, tt.elem, elem)
		}
	}
}

func testParseMention(t *testing.T, expect, elem *lextwt.Mention) {
	t.Helper()

	assert := assert.New(t)

	assert.True(elem != nil)
	assert.Equal(elem.Literal(), expect.Literal())
	assert.Equal(expect.Name(), elem.Name())
	assert.Equal(expect.Domain(), elem.Domain())
	assert.Equal(expect.Target(), elem.Target())
}

type tagTestCase struct {
	lit  string
	elem *lextwt.Tag
	errs []error
}

func TestParseTag(t *testing.T) {
	assert := assert.New(t)

	tests := []tagTestCase{
		{
			lit:  "#<asdfasdf https://sour.is/search?tag=asdfasdf>",
			elem: lextwt.NewTag("asdfasdf", "https://sour.is/search?tag=asdfasdf"),
		},

		{
			lit:  "#<https://sour.is/search?tag=asdfasdf>",
			elem: lextwt.NewTag("", "https://sour.is/search?tag=asdfasdf"),
		},

		{
			lit:  "#asdfasdf",
			elem: lextwt.NewTag("asdfasdf", ""),
		},
	}

	for _, tt := range tests {
		r := strings.NewReader(" " + tt.lit)
		lexer := lextwt.NewLexer(r)
		lexer.NextTok() // remove first token we added to avoid parsing as comment.
		parser := lextwt.NewParser(lexer)
		elem := parser.ParseTag()

		assert.True(parser.IsEOF())
		if len(tt.errs) == 0 {
			testParseTag(t, tt.elem, elem)
		}
	}
}

func testParseTag(t *testing.T, expect, elem *lextwt.Tag) {
	t.Helper()

	assert := assert.New(t)

	assert.True(elem != nil)
	assert.Equal(expect.Literal(), elem.Literal())
	assert.Equal(expect.Text(), elem.Text())
	assert.Equal(expect.Target(), elem.Target())

	url, err := url.Parse(expect.Target())
	eURL, eErr := elem.URL()
	assert.Equal(err, eErr)
	assert.Equal(url, eURL)
}

type subjectTestCase struct {
	lit  string
	elem *lextwt.Subject
	errs []error
}

func TestParseSubject(t *testing.T) {
	assert := assert.New(t)

	tests := []subjectTestCase{
		{
			lit:  "(#<asdfasdf https://sour.is/search?tag=asdfasdf>)",
			elem: lextwt.NewSubjectTag("asdfasdf", "https://sour.is/search?tag=asdfasdf"),
		},

		{
			lit:  "(#<https://sour.is/search?tag=asdfasdf>)",
			elem: lextwt.NewSubjectTag("", "https://sour.is/search?tag=asdfasdf"),
		},

		{
			lit:  "(#asdfasdf)",
			elem: lextwt.NewSubjectTag("asdfasdf", ""),
		},
		{
			lit:  "(re: something)",
			elem: lextwt.NewSubject("re: something"),
		},
	}

	for _, tt := range tests {
		r := strings.NewReader(" " + tt.lit)
		lexer := lextwt.NewLexer(r)
		lexer.NextTok() // remove first token we added to avoid parsing as comment.

		parser := lextwt.NewParser(lexer)

		elem := parser.ParseSubject()

		assert.True(parser.IsEOF())
		if len(tt.errs) == 0 {
			testParseSubject(t, tt.elem, elem)
		}
	}
}

func testParseSubject(t *testing.T, expect, elem *lextwt.Subject) {
	assert := assert.New(t)

	assert.Equal(elem.Literal(), expect.Literal())
	assert.Equal(expect.Text(), elem.Text())
	if tag, ok := expect.Tag().(*lextwt.Tag); ok && tag != nil {
		testParseTag(t, tag, elem.Tag().(*lextwt.Tag))
	}
}

type linkTestCase struct {
	lit  string
	elem *lextwt.Link
	errs []error
}

func TestParseLink(t *testing.T) {
	assert := assert.New(t)

	tests := []linkTestCase{
		{
			lit:  "[asdfasdf](https://sour.is/search?tag=asdfasdf)",
			elem: lextwt.NewLink("asdfasdf", "https://sour.is/search?tag=asdfasdf", lextwt.LinkStandard),
		},

		{
			lit:  "[asdfasdf hgfhgf](https://sour.is/search?tag=asdfasdf)",
			elem: lextwt.NewLink("asdfasdf hgfhgf", "https://sour.is/search?tag=asdfasdf", lextwt.LinkStandard),
		},

		{
			lit:  "![](https://sour.is/search?tag=asdfasdf)",
			elem: lextwt.NewLink("", "https://sour.is/search?tag=asdfasdf", lextwt.LinkMedia),
		},

		{
			lit:  "<https://sour.is/search?tag=asdfasdf>",
			elem: lextwt.NewLink("", "https://sour.is/search?tag=asdfasdf", lextwt.LinkPlain),
		},

		{
			lit:  "https://sour.is/search?tag=asdfasdf",
			elem: lextwt.NewLink("", "https://sour.is/search?tag=asdfasdf", lextwt.LinkNaked),
		},
	}

	for _, tt := range tests {
		r := strings.NewReader(" " + tt.lit)
		lexer := lextwt.NewLexer(r)
		lexer.NextTok() // remove first token we added to avoid parsing as comment.
		parser := lextwt.NewParser(lexer)
		elem := parser.ParseLink()

		assert.True(parser.IsEOF())
		if len(tt.errs) == 0 {
			testParseLink(t, tt.elem, elem)
		}
	}
}
func testParseLink(t *testing.T, expect, elem *lextwt.Link) {
	t.Helper()

	assert := assert.New(t)

	assert.True(elem != nil)
	assert.Equal(expect.Literal(), elem.Literal())
	assert.Equal(expect.Text(), elem.Text())
	assert.Equal(expect.Target(), elem.Target())
}

type twtTestCase struct {
	lit     string
	text    string
	md      string
	html    string
	twt     types.Twt
	subject string
	twter   *types.Twter
}

func TestParseTwt(t *testing.T) {
	twter := types.Twter{Nick: "example", URI: "http://example.com/example.txt"}
	tests := []twtTestCase{
		{
			lit: "2016-02-03T23:03:00+00:00	@<example http://example.org/twtxt.txt>\u2028welcome to twtxt!\n",
			text: "@example\nwelcome to twtxt!",
			md:   "[@example](http://example.org#example)\nwelcome to twtxt!",
			html: `<a href="http://example.org">@example<em>@example.org</em></a>` + "\nwelcome to twtxt!",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2016-02-03T23:03:00+00:00"), "2016-02-03T23:03:00+00:00"),
				lextwt.NewMention("example", "http://example.org/twtxt.txt"),
				lextwt.LineSeparator,
				lextwt.NewText("welcome to twtxt!"),
			),
		},

		{
			lit: "2020-12-25T16:55:57Z	I'm busy, but here's an 1+ [Christmas Tree](https://codegolf.stackexchange.com/questions/4244/code-golf-christmas-edition-how-to-print-out-a-christmas-tree-of-height-n)  ``` . 11+1< (Any unused function name|\"\\\"/1+^<#     \"     (row|\"(Fluff|\"\\\"/^<#               11+\"\"*\"**;               1+           \"\\\"/^<#\"<*)           1           (Mess|/\"\\^/\"\\\"+1+1+^<#               11+\"\"*+\"\"*+;               1+           /\"\\^/\"\\\"+1+1+^<#\"<*)           11+\"\"\"**+;     )     1+ \"\\\"/1+^<#) 11+1<(row) ```",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2020-12-25T16:55:57Z"), "2020-12-25T16:55:57Z"),
				lextwt.NewText("I'm busy, but here's an 1+ "),
				lextwt.NewLink("Christmas Tree", "https://codegolf.stackexchange.com/questions/4244/code-golf-christmas-edition-how-to-print-out-a-christmas-tree-of-height-n", lextwt.LinkStandard),
				lextwt.LineSeparator,
				lextwt.LineSeparator,
				lextwt.NewCode(" . 11+1< (Any unused function name|\"\\\"/1+^<#     \"     (row|\"(Fluff|\"\\\"/^<#               11+\"\"*\"**;               1+           \"\\\"/^<#\"<*)           1           (Mess|/\"\\^/\"\\\"+1+1+^<#               11+\"\"*+\"\"*+;               1+           /\"\\^/\"\\\"+1+1+^<#\"<*)           11+\"\"\"**+;     )     1+ \"\\\"/1+^<#) 11+1<(row) ", lextwt.CodeBlock),
			),
		},

		{
			lit: "2020-12-25T16:57:57Z	@<hirad https://twtxt.net/user/hirad/twtxt.txt> (#<hrqg53a https://twtxt.net/search?tag=hrqg53a>) @<prologic https://twtxt.net/user/prologic/twtxt.txt> make this a blog post plz  And I forgot, [Try It Online Again!](https://tio.run/#jVVbb5tIFH7nV5zgB8DGYJxU7br2Q1IpVausFWXbhxUhCMO4RgszdGbIRZv97d4zYAy2Y7fIRnP5znfuh@JFrhgdr9c9WElZiInrFhGPsxcZPZPMkWW@yLgTs9wtmJDuh/ejD@/eexfn3h9uSiXhBSf4Hi4ZH3rDlA6Lik/TemduKbi7SKlL6CNsjnvgDaAjh2u4ba5uK73wTSkGF74STnK1pTaMR94FIm7SmNCYQCrg0ye4@nv41yVcOCMEX1/egOec4@rz/Dt8vr15PNfSvGBcgngR2pKzHGKWZSSWKaMCNncJ@VkSTRM2iARm9da0bPj3P01LyBIYJUVWClMgdgZz3FoTDfBJl0AZcnNZ7zdnGaEm6nMi/uPRgrMZjNtr9RQcnQf9u4h@kAnoMIAG7Y8C3OngL9OMgGSwIECeSVxKkgT6DokSIc@pND2r1U0LNJAVHf2@F9hgcKMF8)",
			subject: "(#hrqg53a)",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2020-12-25T16:57:57Z"), "2020-12-25T16:57:57Z"),
				lextwt.NewMention("hirad", "https://twtxt.net/user/hirad/twtxt.txt"),
				lextwt.NewText(" "),
				lextwt.NewSubjectTag("hrqg53a", "https://twtxt.net/search?tag=hrqg53a"),
				lextwt.NewText(" "),
				lextwt.NewMention("prologic", "https://twtxt.net/user/prologic/twtxt.txt"),
				lextwt.NewText(" make this a blog post plz"),
				lextwt.LineSeparator,
				lextwt.LineSeparator,
				lextwt.NewText("And I forgot, "),
				lextwt.NewLink("Try It Online Again!", "https://tio.run/#jVVbb5tIFH7nV5zgB8DGYJxU7br2Q1IpVausFWXbhxUhCMO4RgszdGbIRZv97d4zYAy2Y7fIRnP5znfuh@JFrhgdr9c9WElZiInrFhGPsxcZPZPMkWW@yLgTs9wtmJDuh/ejD@/eexfn3h9uSiXhBSf4Hi4ZH3rDlA6Lik/TemduKbi7SKlL6CNsjnvgDaAjh2u4ba5uK73wTSkGF74STnK1pTaMR94FIm7SmNCYQCrg0ye4@nv41yVcOCMEX1/egOec4@rz/Dt8vr15PNfSvGBcgngR2pKzHGKWZSSWKaMCNncJ@VkSTRM2iARm9da0bPj3P01LyBIYJUVWClMgdgZz3FoTDfBJl0AZcnNZ7zdnGaEm6nMi/uPRgrMZjNtr9RQcnQf9u4h@kAnoMIAG7Y8C3OngL9OMgGSwIECeSVxKkgT6DokSIc@pND2r1U0LNJAVHf2@F9hgcKMF8", lextwt.LinkStandard),
			),
		},

		{
			lit: "2020-12-04T21:43:43Z	@<prologic https://twtxt.net/user/prologic/twtxt.txt> (#<63dtg5a https://txt.sour.is/search?tag=63dtg5a>) Web Key Directory: a way to self host your public key. instead of using a central system like pgp.mit.net or OpenPGP.org you have your key on a server you own.   it takes an email@address.com hashes the part before the @ and turns it into `[openpgpkey.]address.com/.well-known/openpgpkey[/address.com]/<hash>`",
			subject: "(#63dtg5a)",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2020-12-04T21:43:43Z"), "2020-12-04T21:43:43Z"),
				lextwt.NewMention("prologic", "https://twtxt.net/user/prologic/twtxt.txt"),
				lextwt.NewText(" "),
				lextwt.NewSubjectTag("63dtg5a", "https://txt.sour.is/search?tag=63dtg5a"),
				lextwt.NewText(" Web Key Directory: a way to self host your public key. instead of using a central system like pgp.mit.net or OpenPGP.org you have your key on a server you own. "),
				lextwt.LineSeparator,
				lextwt.LineSeparator,
				lextwt.NewText("it takes an email@address.com hashes the part before the @ and turns it into "),
				lextwt.NewCode("[openpgpkey.]address.com/.well-known/openpgpkey[/address.com]/<hash>", lextwt.CodeInline),
			),
		},

		{
			lit: "2020-07-20T06:59:52Z	@<hjertnes https://hjertnes.social/twtxt.txt> Is it okay to have two personas :) I have https://twtxt.net/u/prologic and https://prologic.github.io/twtxt.txt 🤔",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2020-07-20T06:59:52Z"), "2020-07-20T06:59:52Z"),
				lextwt.NewMention("hjertnes", "https://hjertnes.social/twtxt.txt"),
				lextwt.NewText(" Is it okay to have two personas :) I have "),
				lextwt.NewLink("", "https://twtxt.net/u/prologic", lextwt.LinkNaked),
				lextwt.NewText(" and "),
				lextwt.NewLink("", "https://prologic.github.io/twtxt.txt", lextwt.LinkNaked),
				lextwt.NewText(" 🤔"),
			),
		},

		{
			lit: `2021-01-21T23:25:59Z	Alligator  ![](https://twtxt.net/media/L6g5PMqA2JXX7ra5PWiMsM)  > Guy says to his colleague “just don’t fall in!” She replies “yeah good advice!”  🤣  #AustraliaZoo`,
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-01-21T23:25:59Z"), "2021-01-21T23:25:59Z"),
				lextwt.NewText("Alligator"),
				lextwt.LineSeparator,
				lextwt.LineSeparator,
				lextwt.NewLink("", "https://twtxt.net/media/L6g5PMqA2JXX7ra5PWiMsM", lextwt.LinkMedia),
				lextwt.LineSeparator,
				lextwt.LineSeparator,
				lextwt.NewText("> Guy says to his colleague “just don’t fall in!” She replies “yeah good advice!”"),
				lextwt.LineSeparator,
				lextwt.LineSeparator,
				lextwt.NewText("🤣"),
				lextwt.LineSeparator,
				lextwt.LineSeparator,
				lextwt.NewTag("AustraliaZoo", ""),
			),
		},

		{
			lit: `2021-01-24T02:19:54Z	(#ezmdswq) @<lyse https://lyse.isobeef.org/twtxt.txt> (#ezmdswq) Looks good for me!  ![](https://txt.sour.is/media/353DzAXLDCv43GofSMw6SL)`,
			subject: "(#ezmdswq)",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-01-24T02:19:54Z"), "2021-01-24T02:19:54Z"),
				lextwt.NewSubjectTag("ezmdswq", ""),
				lextwt.NewText(" "),
				lextwt.NewMention("lyse", "https://lyse.isobeef.org/twtxt.txt"),
				lextwt.NewText(" ("),
				lextwt.NewTag("ezmdswq", ""),
				lextwt.NewText(") Looks good for me!  "),
				lextwt.NewLink("", "https://txt.sour.is/media/353DzAXLDCv43GofSMw6SL", lextwt.LinkMedia),
			),
		},

		{
			lit: `2021-01-18T20:45:57Z	#9c913a	Web UI for Picoblog: I'm thinking of something similar to [Saisho Edit](/saisho-edit). #picoblog`,
			md: "[#9c913a](http://example.org/search?tag=9c913a)	Web UI for Picoblog: I'm thinking of something similar to [Saisho Edit](/saisho-edit). [#picoblog](http://example.org/search?tag=picoblog)",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-01-18T20:45:57Z"), "2021-01-18T20:45:57Z"),
				lextwt.NewTag("9c913a", ""),
				lextwt.NewText("	Web UI for Picoblog: I'm thinking of something similar to "),
				lextwt.NewLink("Saisho Edit", "/saisho-edit", lextwt.LinkStandard),
				lextwt.NewText(". "),
				lextwt.NewTag("picoblog", ""),
			),
		},

		{
			lit: `2021-02-04T12:54:21Z	https://fosstodon.org/@/105673078150704477`,
			md: "https://fosstodon.org/@/105673078150704477",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-02-04T12:54:21Z"), "2021-02-04T12:54:21Z"),
				lextwt.NewLink("", "https://fosstodon.org/@/105673078150704477", lextwt.LinkNaked),
			),
		},

		{
			lit: `2021-02-04T12:54:21Z	@stats.`,
			md: "@stats.",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-02-04T12:54:21Z"), "2021-02-04T12:54:21Z"),
				lextwt.NewMention("stats", ""),
				lextwt.NewText("."),
			),
		},

		{
			lit: `2021-02-04T12:54:21Z	a twt witn (not a) subject`,
			subject: "(#czirbha)",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-02-04T12:54:21Z"), "2021-02-04T12:54:21Z"),
				lextwt.NewText("a twt witn (not a) subject"),
			),
		},

		{
			lit: `2021-02-04T12:54:21Z	@<other http://example.com/other.txt>	example`,
			twter: &types.Twter{Nick: "other", URI: "http://example.com/other.txt"},
			twt: lextwt.NewTwt(
				types.Twter{Nick: "other", URI: "http://example.com/other.txt"},
				lextwt.NewDateTime(parseTime("2021-02-04T12:54:21Z"), "2021-02-04T12:54:21Z"),
				lextwt.NewMention("other", "http://example.com/other.txt"),
				lextwt.NewText("\texample"),
			),
		},

		{
			lit: `2021-02-18T00:44:45Z	(_just kidding!_)`,
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-02-18T00:44:45Z"), "2021-02-18T00:44:45Z"),
				lextwt.NewSubject("_just kidding!_"),
			),
		},

		{
			lit: `2021-11-04T23:20:59Z	multi.    line.    twt..`,
			text: "multi.  \n\nline.  \n\ntwt..",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-11-04T23:20:59Z"), "2021-11-04T23:20:59Z"),
				lextwt.NewText("multi.  "),
				lextwt.LineSeparator,
				lextwt.LineSeparator,
				lextwt.NewText("line.  "),
				lextwt.LineSeparator,
				lextwt.LineSeparator,
				lextwt.NewText("twt.."),
			),
		},

		{
			lit: `2021-11-05T19:40:28Z	> multi > line line`,
			text: "> multi\n> line\nline",
			md:   "> multi\n> line\nline",
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-11-05T19:40:28Z"), "2021-11-05T19:40:28Z"),
				lextwt.NewText("> multi"),
				lextwt.LineSeparator,
				lextwt.NewText("> line"),
				lextwt.LineSeparator,
				lextwt.NewText("line"),
			),
		},

		{
			lit: `2021-11-05T22:00:00+01:00	(#6zqn5bq) @<prologic https://twtxt.net/user/prologic/twtxt.txt> @<fastidious https://arrakis.netbros.com/user/fastidious/twtxt.txt> Thanks guys! :-(). (as to be expected). (@<movq https://www.uninformativ.de/twtxt.txt> told me so ;-))`,
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-11-05T22:00:00+01:00"), "2021-11-05T22:00:00+01:00"),
				lextwt.NewSubjectTag("6zqn5bq", ""),
				lextwt.NewText(" "),
				lextwt.NewMention("prologic", "https://twtxt.net/user/prologic/twtxt.txt"),
				lextwt.NewText(" "),
				lextwt.NewMention("fastidious", "https://arrakis.netbros.com/user/fastidious/twtxt.txt"),
				lextwt.NewText(" Thanks guys! :-(). (as to be expected). ("),
				lextwt.NewMention("movq", "https://www.uninformativ.de/twtxt.txt"),
				lextwt.NewText(" told me so ;-))"),
			),
		},

		{
			lit: `2021-11-05T22:00:00+01:00	![alt](https://example.com/image.png "a title")`,
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-11-05T22:00:00+01:00"), "2021-11-05T22:00:00+01:00"),
				lextwt.NewMedia("alt", "https://example.com/image.png", `"a title"`),
			),
		},

		{
			lit: `2021-11-05T22:00:00+01:00	![alt 'with single quotes'](https://example.com/image.png)`,
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-11-05T22:00:00+01:00"), "2021-11-05T22:00:00+01:00"),
				lextwt.NewMedia("alt 'with single quotes'", "https://example.com/image.png", ""),
			),
		},

		// "👋 Hey @<%s %s/twtxt.txt>, a new user (@<%s %s/twtxt.txt>) has joined your pod %s! 🥳"
		{
			lit: `2021-11-05T22:00:00+01:00	👋 Hey @<foo http://example.com/twtxt.txt>, a new user (@<bar http://example.com/twtxt.txt>) has joined your pod binbaz! 🥳`,
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-11-05T22:00:00+01:00"), "2021-11-05T22:00:00+01:00"),
				lextwt.NewText("👋 Hey "),
				lextwt.NewMention("foo", "http://example.com/twtxt.txt"),
				lextwt.NewText(", a new user ("),
				lextwt.NewMention("bar", "http://example.com/twtxt.txt"),
				lextwt.NewText(") has joined your pod binbaz! 🥳"),
			),
		},

		{
			lit: `2021-11-05T22:00:00+01:00	@foo@example.com. wherefore art thou @foo@example.com?`,
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-11-05T22:00:00+01:00"), "2021-11-05T22:00:00+01:00"),
				lextwt.NewMention("foo@example.com", ""),
				lextwt.NewText(". wherefore art thou "),
				lextwt.NewMention("foo@example.com", ""),
				lextwt.NewText("?"),
			),
		},
	}

	fmtOpts := mockFmtOpts{"http://example.org"}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("TestParseTwt %d", i), func(t *testing.T) {
			assert := assert.New(t)

			r := strings.NewReader(tt.lit)
			lexer := lextwt.NewLexer(r)
			parser := lextwt.NewParser(lexer)
			parser.SetTwter(&twter)
			twt := parser.ParseTwt()

			assert.True(twt != nil)
			if twt != nil {
				assert.Equal(fmt.Sprintf("%L", tt.twt), fmt.Sprintf("%L", twt))

				testParseTwt(t, tt.twt, twt)
			}
			if tt.text != "" {
				assert.Equal(tt.text, twt.FormatText(types.TextFmt, fmtOpts))
			}
			if tt.md != "" {
				assert.Equal(tt.md, twt.FormatText(types.MarkdownFmt, fmtOpts))
			}
			if tt.html != "" {
				assert.Equal(tt.html, twt.FormatText(types.HTMLFmt, fmtOpts))
			}
			if tt.subject != "" {
				assert.Equal(tt.subject, fmt.Sprintf("%c", twt.Subject()))
			}
			if tt.twter != nil {
				assert.Equal(tt.twter.Nick, twt.Twter().Nick)
				assert.Equal(tt.twter.URI, twt.Twter().URI)
			}
		})
	}
}

func testParseTwt(t *testing.T, expect, elem types.Twt) {
	assert := assert.New(t)

	assert.Equal(expect.Twter(), elem.Twter())

	{
		m := elem.Subject()
		n := expect.Subject()
		testParseSubject(t, n.(*lextwt.Subject), m.(*lextwt.Subject))
	}

	{
		m := elem.Mentions()
		n := expect.Mentions()
		assert.Equal(len(n), len(m))
		for i := range m {
			testParseMention(t, m[i].(*lextwt.Mention), n[i].(*lextwt.Mention))
		}
		assert.Equal(n, m)
	}

	{
		m := elem.Tags()
		n := expect.Tags()

		assert.Equal(len(n), len(m))
		for i := range m {
			testParseTag(t, m[i].(*lextwt.Tag), n[i].(*lextwt.Tag))
		}
	}

	{
		m := elem.Links()
		n := expect.Links()

		assert.Equal(len(n), len(m))
		for i := range m {
			testParseLink(t, m[i].(*lextwt.Link), n[i].(*lextwt.Link))
		}
	}

	{
		m := elem.(*lextwt.Twt).Elems()
		n := expect.(*lextwt.Twt).Elems()
		for i, e := range m {
			if i > len(n) {
				break
			}

			switch elem := e.(type) {
			case *lextwt.Mention:
				expect, ok := n[i].(*lextwt.Mention)
				assert.True(ok)
				testParseMention(t, elem, expect)
			case *lextwt.Tag:
				expect, ok := n[i].(*lextwt.Tag)
				assert.True(ok)
				testParseTag(t, elem, expect)
			case *lextwt.Link:
				expect, ok := n[i].(*lextwt.Link)
				assert.True(ok)
				testParseLink(t, elem, expect)
			case *lextwt.Subject:
				expect, ok := n[i].(*lextwt.Subject)
				assert.True(ok)
				testParseSubject(t, elem, expect)

			default:
				assert.Equal(e, n[i])
			}
		}
		assert.Equal(len(m), len(n)) // len(elem) == len(expect)
	}

	assert.Equal(fmt.Sprintf("%+l", expect), fmt.Sprintf("%+l", elem))
}

type commentTestCase struct {
	lit   string
	key   string
	value string
}

func TestParseComment(t *testing.T) {
	assert := assert.New(t)

	tests := []commentTestCase{
		{lit: "# comment\n"},
		{lit: "# key = value\n",
			key: "key", value: "value"},
		{lit: "# key with space = value with space\n",
			key: "key with space", value: "value with space"},
		{lit: "# follower = xuu@sour.is https://sour.is/xuu.txt\n",
			key: "follower", value: "xuu@sour.is https://sour.is/xuu.txt"},
		{lit: "# no-key-without-value-but-trailing-space = \n"},
		{lit: "# no-key-without-value-and-no-trailing-space =\n"},
		{lit: "#key-without-space-in-front = value\n",
			key: "key-without-space-in-front", value: "value"},
		{lit: "# key-without-space-afterwards= value\n",
			key: "key-without-space-afterwards", value: "value"},
		{lit: "# key =value without space in front\n",
			key: "key", value: "value without space in front"},
		{lit: "# key-without-space-afterwards=value without space in front\n",
			key: "key-without-space-afterwards", value: "value without space in front"},
		{lit: "#key=value\n",
			key: "key", value: "value"},
		{lit: "# key = val#ue\n",
			key: "key", value: "val#ue"},
		{lit: "# key = value with special chars #  = 123 ! 08:00T (<[bla]>)@<> \n",
			key: "key", value: "value with special chars #  = 123 ! 08:00T (<[bla]>)@<>"},
		{lit: "#=no key value, key is missing\n"},
		{lit: "# =no key value, key is missing\n"},
		{lit: "# # no key = no value\n"},
		{lit: "##=no key value\n"},
		{lit: "# no_key_with_empty_value =\n"},
		{lit: "#no_key_with_empty_value=  \n"},
		{lit: "# description   = best feed ever\u2028so says everybody\n",
			key: "description", value: "best feed ever\u2028so says everybody"},
		{lit: "# link = My blog https://example.com/blog/?q=twtxt&sort=year#anchor\n",
			key: "link", value: "My blog https://example.com/blog/?q=twtxt&sort=year#anchor"},
	}
	for _, tt := range tests {
		r := strings.NewReader(tt.lit)
		lexer := lextwt.NewLexer(r)
		parser := lextwt.NewParser(lexer)

		elem := parser.ParseComment()

		assert.True(elem != nil) // not nil
		if elem != nil {
			assert.Equal([]byte(tt.lit), []byte(elem.Literal())) // literal mismatch
			assert.Equal(tt.key, elem.Key())                     // key mismatch
			assert.Equal(tt.value, elem.Value())                 // value mismatch
		}
	}
}

type textTestCase struct {
	lit   string
	elems []*lextwt.Text
}

func TestParseText(t *testing.T) {
	assert := assert.New(t)

	tests := []textTestCase{
		{
			lit: "@ ",
			elems: []*lextwt.Text{
				lextwt.NewText("@ "),
			},
		},
	}
	for _, tt := range tests {
		r := strings.NewReader(tt.lit)
		lexer := lextwt.NewLexer(r)
		parser := lextwt.NewParser(lexer)

		var lis []lextwt.Elem
		for elem := parser.ParseElem(); elem != nil; elem = parser.ParseElem() {
			lis = append(lis, elem)
		}

		assert.Equal(len(tt.elems), len(lis))
		for i, expect := range tt.elems {
			assert.Equal(expect, lis[i])
		}
	}
}

func parseTime(s string) time.Time {
	if dt, err := time.Parse(time.RFC3339, s); err == nil {
		return dt
	}
	return time.Time{}
}
