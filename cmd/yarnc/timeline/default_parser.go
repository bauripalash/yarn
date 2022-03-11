package timeline

import (
	"fmt"
	"time"

	"go.yarn.social/types"
)

type defaultParser struct {
}

func (d defaultParser) Parse(twts types.Twts, me types.Twter) error {
	for _, twt := range twts {
		PrintTwt(twt, time.Now(), me)
		fmt.Println()
	}
	return nil
}

type defaultRawParser struct {
}

func (d defaultRawParser) Parse(twts types.Twts, me types.Twter) error {
	for _, twt := range twts {
		PrintTwtRaw(twt)
	}

	return nil
}
