package lextwt_test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"git.mills.io/yarnsocial/yarn/types"
	"git.mills.io/yarnsocial/yarn/types/lextwt"
)

type fileTestCase struct {
	in       io.Reader
	twter    *types.Twter
	override *types.Twter
	out      types.TwtFile
	err      error
}

func TestParseFile(t *testing.T) {
	assert := assert.New(t)

	twter := types.Twter{Nick: "example", URI: "https://example.com/twtxt.txt"}
	override := types.Twter{
		Nick:       "override",
		URI:        "https://example.com/twtxt.txt",
		HashingURI: "https://example.com/twtxt.txt",
		Following:  1,
		Follow:     map[string]types.Twter{"xuu@txt.sour.is": {Nick: "xuu@txt.sour.is", URI: "https://txt.sour.is/users/xuu.txt"}},
		Metadata: url.Values{
			"url":     []string{"https://example.com/twtxt.txt"},
			"nick":    []string{"override"},
			"follows": []string{"xuu@txt.sour.is https://txt.sour.is/users/xuu.txt"},
		},
	}

	tests := []fileTestCase{
		// Test1: Empty Feed
		{
			twter:    &twter,
			override: &override,
			in: strings.NewReader(`# My Twtxt!
# nick = override
# url = https://example.com/twtxt.txt
# follows = xuu@txt.sour.is https://txt.sour.is/users/xuu.txt
#
`),
			out: lextwt.NewTwtFile(
				override,

				lextwt.Comments{
					lextwt.NewComment("# My Twtxt!"),
					lextwt.NewCommentValue("# nick = override", "nick", "override"),
					lextwt.NewCommentValue("# url = https://example.com/twtxt.txt", "url", "https://example.com/twtxt.txt"),
					lextwt.NewCommentValue("# follows = xuu@txt.sour.is https://txt.sour.is/users/xuu.txt", "follows", "xuu@txt.sour.is https://txt.sour.is/users/xuu.txt"),
					lextwt.NewComment("#"),
				},
				nil,
			),
			err: nil,
		},
		// Test1: Empty Feed with empty lines
		{
			twter:    &twter,
			override: &override,
			in: strings.NewReader(`# My Twtxt!
# nick = override
# url = https://example.com/twtxt.txt
# follows = xuu@txt.sour.is https://txt.sour.is/users/xuu.txt
#

`),
			out: lextwt.NewTwtFile(
				override,

				lextwt.Comments{
					lextwt.NewComment("# My Twtxt!"),
					lextwt.NewCommentValue("# nick = override", "nick", "override"),
					lextwt.NewCommentValue("# url = https://example.com/twtxt.txt", "url", "https://example.com/twtxt.txt"),
					lextwt.NewCommentValue("# follows = xuu@txt.sour.is https://txt.sour.is/users/xuu.txt", "follows", "xuu@txt.sour.is https://txt.sour.is/users/xuu.txt"),
					lextwt.NewComment("#"),
				},
				nil,
			),
			err: nil,
		},
		{
			twter:    &twter,
			override: &override,
			in: strings.NewReader(`# My Twtxt!
# nick = override
# url = https://example.com/twtxt.txt
# follows = xuu@txt.sour.is https://txt.sour.is/users/xuu.txt

2016-02-03T23:05:00Z	@<example http://example.org/twtxt.txt>` + "\u2028" + `welcome to twtxt!
22016-0203	ignored
2020-12-02T01:04:00Z	This is an OpenPGP proof that connects my OpenPGP key to this Twtxt account. See https://key.sour.is/id/me@sour.is for more.  [Verifying my OpenPGP key: openpgp4fpr:20AE2F310A74EA7CEC3AE69F8B3B0604F164E04F]
2020-11-13T16:13:22+01:00	@<prologic https://twtxt.net/user/prologic/twtxt.txt> (#<pdrsg2q https://twtxt.net/search?tag=pdrsg2q>) Thanks!
`),
			out: lextwt.NewTwtFile(
				override,

				lextwt.Comments{
					lextwt.NewComment("# My Twtxt!"),
					lextwt.NewCommentValue("# nick = override", "nick", "override"),
					lextwt.NewCommentValue("# url = https://example.com/twtxt.txt", "url", "https://example.com/twtxt.txt"),
					lextwt.NewCommentValue("# follows = xuu@txt.sour.is https://txt.sour.is/users/xuu.txt", "follows", "xuu@txt.sour.is https://txt.sour.is/users/xuu.txt"),
				},

				[]types.Twt{
					lextwt.NewTwt(
						override,
						lextwt.NewDateTime(parseTime("2016-02-03T23:05:00Z"), "2016-02-03T23:05:00Z"),
						lextwt.NewMention("example", "http://example.org/twtxt.txt"),
						lextwt.LineSeparator,
						lextwt.NewText("welcome to twtxt"),
						lextwt.NewText("!"),
					),

					lextwt.NewTwt(
						override,
						lextwt.NewDateTime(parseTime("2020-12-02T01:04:00Z"), "2020-12-02T01:04:00Z"),
						lextwt.NewText("This is an OpenPGP proof that connects my OpenPGP key to this Twtxt account. See "),
						lextwt.NewLink("", "https://key.sour.is/id/me@sour.is", lextwt.LinkNaked),
						lextwt.NewText(" for more."),
						lextwt.LineSeparator,
						lextwt.LineSeparator,
						lextwt.NewText("[Verifying my OpenPGP key: openpgp4fpr:20AE2F310A74EA7CEC3AE69F8B3B0604F164E04F]"),
					),

					lextwt.NewTwt(
						override,
						lextwt.NewDateTime(parseTime("2020-11-13T16:13:22+01:00"), "2020-11-13T16:13:22+01:00"),
						lextwt.NewMention("prologic", "https://twtxt.net/user/prologic/twtxt.txt"),
						lextwt.NewText(" "),
						lextwt.NewSubjectTag("pdrsg2q", "https://twtxt.net/search?tag=pdrsg2q"),
						lextwt.NewText(" Thanks"),
						lextwt.NewText("!"),
					),
				},
			),
		},
		{
			twter: &twter,
			in:    strings.NewReader(`2016-02-03`),
			out: lextwt.NewTwtFile(
				twter,
				nil,
				[]types.Twt{},
			),
			err: types.ErrInvalidFeed,
		},
	}
	for _, tt := range tests {
		f, err := lextwt.ParseFile(tt.in, tt.twter)
		if tt.err != nil {
			assert.True(err == tt.err)
			assert.True(f == nil)
			continue
		} else {
			assert.NoError(err)
			continue
		}

		assert.True(err == nil)
		assert.True(f != nil)

		if tt.override != nil {
			assert.Equal(tt.override, f.Twter())
		}

		{
			lis := f.Info().GetAll("")
			expect := tt.out.Info().GetAll("")
			assert.Equal(len(expect), len(lis))

			for i := range expect {
				assert.Equal(expect[i].Key(), lis[i].Key())
				assert.Equal(expect[i].Value(), lis[i].Value())
			}

			assert.Equal(f.Info().String(), tt.out.Info().String())
		}

		{
			lis := f.Twts()
			expect := tt.out.Twts()
			assert.Equal(len(expect), len(lis))
			for i := range expect {
				testParseTwt(t, expect[i], lis[i])
			}
		}

	}
}

type testExpandLinksCase struct {
	twt    types.Twt
	target *types.Twter
}

func TestExpandLinks(t *testing.T) {
	twter := types.Twter{Nick: "example", URI: "http://example.com/example.txt"}
	conf := mockFmtOpts{
		localURL: "http://example.com",
	}

	tests := []testExpandLinksCase{
		{
			twt: lextwt.NewTwt(
				twter,
				lextwt.NewDateTime(parseTime("2021-01-24T02:19:54Z"), "2021-01-24T02:19:54Z"),
				lextwt.NewMention("@asdf", ""),
			),
			target: &types.Twter{Nick: "asdf", URI: "http://example.com/asdf.txt"},
		},
	}

	assert := assert.New(t)

	for _, tt := range tests {
		lookup := types.FeedLookupFn(func(s string) *types.Twter { return tt.target })
		tt.twt.ExpandMentions(conf, lookup)
		assert.Equal(tt.twt.Mentions()[0].Twter().Nick, tt.target.Nick)
		assert.Equal(tt.twt.Mentions()[0].Twter().URI, tt.target.URI)
	}
}

type mockFmtOpts struct {
	localURL string
}

func (m mockFmtOpts) LocalURL() *url.URL { u, _ := url.Parse(m.localURL); return u }
func (m mockFmtOpts) IsLocalURL(url string) bool {
	return strings.HasPrefix(url, m.localURL)
}
func (m mockFmtOpts) UserURL(url string) string {
	if strings.HasSuffix(url, "/twtxt.txt") {
		return strings.TrimSuffix(url, "/twtxt.txt")
	}
	return url
}
func (m mockFmtOpts) ExternalURL(nick, uri string) string {
	return fmt.Sprintf(
		"%s/external?uri=%s&nick=%s",
		strings.TrimSuffix(m.localURL, "/"),
		uri, nick,
	)
}
func (m mockFmtOpts) URLForTag(tag string) string {
	return fmt.Sprintf(
		"%s/search?tag=%s",
		strings.TrimSuffix(m.localURL, "/"),
		tag,
	)
}
func (m mockFmtOpts) URLForUser(username string) string {
	return fmt.Sprintf(
		"%s/user/%s/twtxt.txt",
		strings.TrimSuffix(m.localURL, "/"),
		username,
	)
}

func TestInvalidFeed(t *testing.T) {
	assert := assert.New(t)

	testCases := []struct {
		Twter types.Twter
		Input string
		Error error
	}{
		// Junk feed (HTML doc?!)
		{
			Twter: types.NewTwter("foo", "https://foo.bar"),
			Input: `<html>\n<title>Foo</title><body>Bar</body>\n<html>`,
			Error: types.ErrInvalidFeed,
		},
		// Junk feed (`yarnd` View?! wtf?!)
		{
			Twter: types.NewTwter("twtxt-net-external", "https://twtxt.net/external?nick=lyse&uri=https%3A//lyse.isobeef.org/user/lyse/twtxt.txt"),
			Input: `<!DOCTYPE html>
<html lang="en">
<head>
<link href="/css/69ca474/yarn.min.css" rel="stylesheet" />
<link rel="icon" type="image/png" href="/img/69ca474/favicon.png" />
<link rel="webmention" href="/webmention" />
<meta name="yarn-uri" content="/user/%s/twtxt.txt" />
<link rel="alternate" type="application/atom&#43;xml" title="twtxt.net local feed" href="https://twtxt.net/atom.xml" />
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<title>twtxt.net External profile for @&lt;lyse https://lyse.isobeef.org/user/lyse/twtxt.txt&gt;</title>
<meta name="author" content="Yarn.social">
<meta name="keywords" content="twtxt, twt, yarn, blog, micro-blog, microblogging, social, media, decentralised, pod">
<meta name="description" content="twtxt.net is the first Yarn.social pod owned and operated by James Mills / prologic -- 🧶 Yarn.social is a Self-Hosted, Twitter™-like Decentralised microBlogging platform. No ads, no tracking, your content, your daa!">
<meta property="og:description" content="twtxt.net is the first Yarn.social pod owned and operated by James Mills / prologic -- 🧶 Yarn.social is a Self-Hosted, Twitter™-like Decentralised microBlogging platform. No ads, no tracking, your content, your daa!">
<meta property="og:site_name" content="twtxt.net">
<meta name="twitter:card" content="summary" />
<meta name="twitter:site" content="Yarn.social" />
<meta name="twitter:description" content="twtxt.net is the first Yarn.social pod owned and operated by James Mills / prologic -- 🧶 Yarn.social is a Self-Hosted, Twitter™-like Decentralised microBlogging platform. No ads, no tracking, your content, your daa!" />
</head>
<body class="preload">
<nav id="mainNav">
<ul id="podLogo">
<li class="podLogo">
<a href="/"><svg width="210px" height="70px" aria-hidden="true" viewBox="0 0 210 70" xmlns="http://www.w3.org/2000/svg">
<g>
<text letter-spacing="2px" font-weight="bolder" font-family="-apple-system, BlinkMacSystemFont, 'egoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif, 'Apple Color Emoji', 'Segoe UI Emoji', 'Segoe UI Symbol', 'Noto Color Emoji'" text-anchor="middle" text-rendering="geometricPrecision" transform="matrix(0.573711, 0, 0, 0.74566, 41.630024, 46.210407)" font-size="35" x="137.16561" y="-9.908" fill="currentColor" stroke="null" id="svg_3" style="white-space: pre;">twtxt.net</text>
<text font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif, 'Apple Color Emoji', 'Segoe UI Emoji', 'Segoe UI Symbol', 'Noto Color Emoji'" stroke-width="0" fill="currentColor" stroke="null" font-size="22" y="54.92" x="80.674" id="svg_4" style="font-size: 13px;">a Yarn.social pod</text>
<circle fill-opacity="0.0" cx="35.997" cy="36.087" r="31.699" />
<path fill="currentColor" d="M 23.787 55.282 C 18.211 55.282 14.55 54.611 14.278 54.56 C 13.205 54.354 12.5 53.32 12.703 52.248 C 12.905 51.172 13.941 50.465 15.013 50.666 C 15.13 50.691 26.62 52.771 39.939 49.28 C 53.236 45.792 61.041 38.43 61.12 38.357 C 61.905 37.601 63.161 37.628 63.921 38.419 C 64.676 39.21 64.649 40.463 63.858 41.22 C 63.512 41.551 55.227 49.366 40.945 53.114 C 34.462 54.81 28.453 55.282 23.787 55.282 Z" />
<path fill="currentColor" d="M 20.414 48.389 C 13.616 48.389 8.639 47.636 8.274 47.58 C 7.194 47.411 6.456 46.399 6.624 45.317 C 6.791 44.237 7.798 43.503 8.885 43.663 C 9.035 43.689 24.126 45.958 37.5 42.449 C 50.819 38.957 61.962 28.788 62.075 28.687 C 62.88 27.947 64.132 27.999 64.873 28.801 C 65.614 29.607 65.564 30.861 64.758 31.602 C 64.277 32.047 52.77 42.543 38.505 46.284 C 32.255 47.924 25.771 48.389 20.414 48.389 Z" />
<path fill="currentColor" d="M 18.555 40.487 C 12.404 40.487 8.117 39.798 7.798 39.747 C 6.72 39.569 5.991 38.55 6.168 37.471 C 6.344 36.392 7.354 35.644 8.444 35.839 C 8.577 35.86 22.041 38.005 35.395 34.503 C 48.694 31.015 58.505 21.738 58.599 21.644 C 59.386 20.888 60.644 20.913 61.4 21.707 C 62.157 22.497 62.129 23.749 61.34 24.506 C 60.911 24.919 50.682 34.592 36.398 38.338 C 29.979 40.016 23.632 40.487 18.555 40.487 Z" />
<path fill="currentColor" d="M 19.045 33.246 C 18.096 33.246 17.255 32.561 17.093 31.595 C 14.752 17.663 19.969 11.016 20.192 10.742 C 20.881 9.891 22.129 9.76 22.977 10.447 C 23.825 11.132 23.96 12.37 23.283 13.22 C 23.199 13.331 18.991 18.977 21 30.936 C 21.18 32.014 20.453 33.036 19.373 33.219 C 19.265 33.238 19.153 33.246 19.045 33.246 Z M 27.422 32.766 C 26.429 32.766 25.572 32.019 25.458 31.009 C 23.615 14.757 28.488 7.879 28.698 7.595 C 29.347 6.711 30.583 6.519 31.467 7.167 C 32.344 7.81 32.539 9.04 31.905 9.922 C 31.826 10.036 27.755 16.104 29.394 30.56 C 29.517 31.647 28.736 32.629 27.649 32.75 C 27.573 32.763 27.497 32.766 27.422 32.766 Z M 36.117 30.56 C 35.132 30.56 34.278 29.824 34.154 28.824 C 32.488 15.409 36.318 8.128 36.482 7.826 C 37.005 6.864 38.206 6.505 39.167 7.026 C 40.126 7.545 40.485 8.743 39.972 9.704 C 39.901 9.838 36.597 16.356 38.082 28.336 C 38.217 29.422 37.447 30.414 36.362 30.547 C 36.281 30.556 36.199 30.56 36.117 30.56 Z M 45.322 26.213 C 44.36 26.213 43.515 25.512 43.366 24.531 C 41.858 14.646 43.836 9.902 43.92 9.705 C 44.349 8.698 45.524 8.235 46.52 8.669 C 47.52 9.098 47.986 10.256 47.564 11.256 C 47.52 11.363 45.979 15.4 47.28 23.934 C 47.447 25.014 46.705 26.025 45.621 26.191 C 45.524 26.207 45.422 26.213 45.322 26.213 Z M 30.296 64.815 C 30.048 64.815 29.796 64.768 29.553 64.671 C 27.109 63.678 24.862 61.457 24.614 61.207 C 23.847 60.43 23.852 59.18 24.625 58.407 C 25.4 57.635 26.654 57.639 27.426 58.414 C 27.948 58.938 29.608 60.419 31.043 61 C 32.057 61.411 32.545 62.565 32.133 63.58 C 31.822 64.35 31.078 64.815 30.296 64.815 Z M 41.594 65.123 C 41.247 65.123 40.895 65.033 40.576 64.842 C 37.872 63.215 34.493 59.901 34.352 59.763 C 33.569 58.995 33.561 57.74 34.329 56.961 C 35.098 56.179 36.352 56.173 37.132 56.938 C 37.164 56.969 40.317 60.062 42.617 61.442 C 43.556 62.007 43.858 63.222 43.297 64.16 C 42.923 64.78 42.267 65.123 41.594 65.123 Z M 50.173 61.793 C 49.855 61.793 49.535 61.718 49.234 61.555 C 46.969 60.335 44.549 57.732 44.278 57.438 C 43.538 56.634 43.593 55.382 44.397 54.641 C 45.204 53.907 46.454 53.956 47.198 54.76 C 47.805 55.421 49.692 57.304 51.115 58.071 C 52.079 58.59 52.44 59.792 51.918 60.754 C 51.558 61.415 50.879 61.793 50.173 61.793 Z M 56.998 56.261 C 56.693 56.261 56.385 56.193 56.099 56.045 C 54.901 55.433 53.546 54.377 53.396 54.261 C 52.535 53.585 52.388 52.34 53.064 51.481 C 53.738 50.624 54.985 50.471 55.843 51.145 C 56.17 51.401 57.178 52.148 57.9 52.518 C 58.877 53.015 59.261 54.21 58.764 55.185 C 58.413 55.866 57.719 56.261 56.998 56.261 Z M 10.653 33.583 C 9.676 33.583 8.828 32.862 8.693 31.868 C 7.985 26.652 9.374 21.707 10.479 19.651 C 10.998 18.689 12.2 18.329 13.164 18.848 C 14.126 19.366 14.487 20.569 13.968 21.532 C 13.269 22.828 12.02 26.928 12.618 31.335 C 12.766 32.419 12.008 33.416 10.922 33.563 C 10.829 33.578 10.741 33.583 10.653 33.583 Z M 53.508 22.574 C 52.575 22.574 51.744 21.91 51.564 20.962 C 51.014 18.051 51.343 14.725 51.955 12.944 C 52.309 11.912 53.435 11.356 54.473 11.714 C 55.507 12.071 56.059 13.198 55.702 14.232 C 55.332 15.307 55.013 17.876 55.458 20.221 C 55.662 21.295 54.957 22.333 53.881 22.538 C 53.754 22.563 53.63 22.574 53.508 22.574 Z" />
</g>
</svg></a>
</li>
</ul>
<ul id="podMobile">
<li class="podMobile">
<a id="burgerMenu" href="javascript:void(0);"><i class="ti ti-menu-2"></i></a>
</li>
</ul>
<ul id="podMenu">
<li class="loginBtn">
<a href="/login">
<i class="ti ti-door-enter"></i> Login
</a>
</li>
<li class="registerBtn">
<a href="/register">
<i class="ti ti-user-plus"></i> Register
</a>
</li>
</ul>
</nav>
<main class="container">
<div class="profile-name">
<span class="p-name p-name-profile">lyse</span>
<span class="p-org p-org-profile">lyse.isobeef.org</span>
</div>
<div class="profile-stats">
<a href="/external?uri=https%3a%2f%2flyse.isobeef.org%2fuser%2flyse%2ftwtxt.txt&nick=lyse" class="u-url">
<i class="ti ti-rss" style="font-size:3em"></i>
</a>
<div>
<a href="/externalFollowing?uri=https%3a%2f%2flyse.isobeef.org%2fuser%2flyse%2ftwtxt.txt"><strong>Following</strong><br />0</a>
</div>
<div>
<a href="#" title="Details on followers are not available on external feeds"><strong>Followers</strong><br />0</a>
</div>
</div>
<div class="profile-info">
<p class="profile-tagline"></p>
</div>
<div class="profile-links">
<a target="_blank" href="https://lyse.isobeef.org/user/lyse/twtxt.txt"><i class="ti ti-link-profile"></i> Twtxt</a>
<a target="_blank" href="https://lyse.isobeef.org/user/lyse/atom.xml"><i class="ti ti-rss-profile"></i> Atom</a>
<a href="https://lyse.isobeef.org/user/lyse/bookmarks"><i class="ti ti-bookmarks"></i> Bookmarks</a>
<a target="_blank" href="https://lyse.isobeef.org/user/lyse/config.yaml"><i class="ti ti-settings"></i> Config</a>
</div>
<div class="profile-recent">
<h2>Recent twts from lyse</h2>
</div>
<div class="h-feed-empty"></div>
</main>
<footer class="container">
<div class="footer-menu">
<a href="/about" class="menu-item">About</a>
<a href="/privacy" class="menu-item">Privacy</a>
<a href="/abuse" class="menu-item">Abuse</a>
<a href="/help" class="menu-item">Help</a>
<a href="/support" class="menu-item">Support</a>
<a href="/atom.xml" class="menu-item"><i class="ti ti-rss"></i></a>
</div>
<div class="footer-copyright">
Running <a href="https://git.mills.io/yarnsocial/yarn" target="_blank">yarnd</a>
<a href="/info"><span class="__cf_email__" data-cfemail="f3c3ddc2c1ddc3b3c5ca9092c7c4c7">[email&#160;protected]</span></a> &mdash;
a <a href="https://yarn.social" target="_blank">Yarn.social</a> pod.
</div>
</footer>
<script data-cfasync="false" src="/cdn-cgi/scripts/5c5dd728/cloudflare-static/email-decode.min.js"></script><script type="e305013a0226ea5a7d0d1baf-application/javascript" src="/js/69ca474/yarn.min.js"></script>
<script src="/cdn-cgi/scripts/7d0fa10a/cloudflare-static/rocket-loader.min.js" data-cf-settings="e305013a0226ea5a7d0d1baf-|49" defer=""></script></body>
</html>`,
			Error: types.ErrInvalidFeed,
		},
		// Junk feed (Using spaces as delimiter?!)
		{
			Twter: types.NewTwter("foo", "https://foo.bar"),
			Input: `# nick = foo
2022-01-01T18:46:17+10:00.   Hello?
`,
			Error: types.ErrInvalidFeed,
		},
		// Junk feed (total garbage?!)lh(
		{
			Twter: types.NewTwter("foo", "https://foo.bar"),
			Input: `This feed is total junk
And basically just contains some rubbish test file!`,
			Error: types.ErrInvalidFeed,
		},
	}

	for _, testCase := range testCases {
		tf, err := lextwt.ParseFile(strings.NewReader(testCase.Input), &testCase.Twter)
		assert.Nil(tf)
		assert.Error(err)
		assert.ErrorIs(err, types.ErrInvalidFeed)
	}
}

func TestSomethingWeird(t *testing.T) {
	twter := &types.Twter{Nick: "foo", URI: "https://lyse.isobeef.org/user/lyse/twtxt.txt"}
	res, err := http.Get("https://lyse.isobeef.org/user/lyse/twtxt.txt")
	if err != nil {
		t.Fail()
	}

	defer res.Body.Close()

	b, _ := ioutil.ReadAll(res.Body)

	l, err := lextwt.ParseFile(bytes.NewReader(b), twter)
	_ = l
	if err != nil {
		t.Fail()
	}
}
