---
layout: page
title: "Multiline Extension"
category: doc
date: 2022-02-09 11:15:00
order: 3
---

At [twtxt.net](https://twtxt.net) the **Multiline** was invented as an
extension to the original [Twtxt File Format Specification](https://twtxt.readthedocs.io/en/latest/user/twtxtfile.html#format-specification).

## Purpose

Since the twtxt file format requires that each post ends in a `\n` new line
character, tweets cannot contain any new line characters. To allow multiline
posts, the **Multiline** extension allows for `\u2028` to be used instead.

Twtxt posters may wish to post paragraphs, or if using markdown, code blocks,
which require multiple lines per post. To prevent the `\n` or `\n\r` characters
from breaking the twtxt file, a non-reserved line break character can be used
to indicate that a post should be rendered with a line break, without breaking
the feed. Clients that are aware of this extension will render the post with
multiple lines, while any clients that are unaware of the extension, or have it
disabled, will mearly treat the line break character as any other unicode
character. This is why [`\u2028`](https://codepoints.net/U+2028) is used, it is
language and script neutral, and has a well defined meaning as a line break. Any
client that renders unicode correctly will probably support this extension
automatically.

Note that the original twtxt client uses [`str.splitlines()`](https://docs.python.org/3/library/stdtypes.html#str.splitlines)
to split tweets, which will also split on any `\u2028` characters. This is a bug
in the client, not the specification nor the extension, as the twtxt.txt file
format specifically uses `\n` as a tweet separator.

## Format

Whenever a `\u2028` codepoint occurs in a tweet, the client will render a line
break at that location.

Each client should render the line break as is most appropriate in its usage
context, for example, in a terminal based client `\u2028` could be replaced by
a `\n`, or in a browser/web based client, the `\u2028` could be replaced with
`<br />`.

```
2021-12-06T14:38:10+13:00	Hello World!\u2028Welcome to twtxt!
```

Becomes:

```
Hello World!
Welcome to twtxt!
```
