package langs

import "embed"

//go:generate goi18n merge active.*.toml translate.*.toml

//go:embed *.toml
var LocaleFS embed.FS
