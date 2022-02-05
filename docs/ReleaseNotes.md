# yarnd v0.13 Aluminium Amarok

Hello Yarners! 🤗

What a month (_and a bit_) it has been! 😅 During this time we've closed out
78 Pull Requests, 55 Issues from the hard work of 8 contributors over 174 commits!

This release `yarnd v0.13` is **special** because this will be the last release
for a while. The project will be entering a "feature freeze" whilst the developers
and active contributors slow down a bit and work on some other much-needed components.

We will be working on improving documentation, deployment guides, builtin pages
and other supporting services and components in the ecosystem. In addition a
decision has been made to relicense all software components under a new license
(AGPLv3) going forward. We hope this will not affect contributions in any way but
will also serve to protect what we've all worked so hard to build.

But don't worry! We will still be committing to `main`, we just won't be adding
and new significant new features for a while.

Without further ado, here are the following changes for our 0.12 release!

## Highlights

First the important noteworthy bug-fixes: 🐞

- Fixed various rendering issues with `blockquote`(s) and code snippets.
- Fixed image alt and title rendering
- Fixed various Cache consistency bugs
- Fixed privacy issue in "Mentions" view
- Fixed support for WebMentions and cross-pod mentions
- Fixed weird behaviour when accidentally posting an empty Twt

And finally the new shiny new features! 🥳

- Pod Owner/Operators (_Poderators_) can now edit Pod pages in `data/pages/<name>.md` and changes are reflected live!
- Users that reply to someone they don't follow now correctly @-mention them.
- Users can now @-mention cross-pod as well as any Twtxt-supported feed using the now well supported syntax @nick@domain
- Added first-class support for GIF(s) 🦋 Finally! 😅
- Added support for fetching feeds over gemini://
- Added support for Twitter Summary Card
- Added support for pressing Escape to cancel a Reply
- Added support for configuring Twts Per Page in Manage Pod

----

As per usual, please provide feedback to @prologic or reply to this Yarn 🤗
