package timeline

import "go.yarn.social/types"

type Parser interface {
	Parse(twts types.Twts, me types.Twter) error
}

type Options struct {
	OutputJSON bool
	OutputRAW  bool
	OutputGIT  bool
}

// GetParser Factory ...
func GetParser(opts Options) Parser {
	switch {
	case opts.OutputJSON:
		return jsontParser{}
	case opts.OutputRAW:
		return defaultRawParser{}
	case opts.OutputGIT:
		return gitParser{defaultPrinter{}}
	default:
		return defaultParser{}
	}
}
