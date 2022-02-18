// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"

	"git.mills.io/yarnsocial/yarn"
	"git.mills.io/yarnsocial/yarn/internal"
	sync "github.com/sasha-s/go-deadlock"
)

var (
	debug   bool
	version bool
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.BoolVarP(&debug, "debug", "D", false, "enable debug logging")
	flag.BoolVarP(&version, "version", "v", false, "display version information")
}

func main() {
	flag.Parse()

	if version {
		fmt.Printf("%s %s", filepath.Base(os.Args[0]), yarn.FullVersion())
		os.Exit(0)
	}

	if debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)

		// Disable deadlock detection in production mode
		sync.Opts.Disable = true
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	fn := flag.Arg(0)
	if !internal.FileExists(fn) {
		log.Errorf("cache not found %s", fn)
		os.Exit(1)
	}

	conf := internal.NewConfig()
	cache, err := internal.LoadCacheFromFile(conf, fn)
	if err != nil {
		log.WithError(err).Errorf("error loading cache: %s", fn)
		os.Exit(2)
	}

	data, err := json.Marshal(cache)
	if err != nil {
		log.WithError(err).Error("error dumping cache")
		os.Exit(3)
	}

	fmt.Println(string(data))
}
