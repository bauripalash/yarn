// Copyright 2020-present Yarn.social
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net"
	"os"
	"strconv"

	log "github.com/sirupsen/logrus"
	"github.com/tebeka/selenium"
)

func main() {
	port, err := pickUnusedPort()
	if err != nil {
		log.WithError(err).Error("error picking port")
		os.Exit(1)
	}

	var opts []selenium.ServiceOption
	service, err := selenium.NewChromeDriverService("chromedriver", port, opts...)
	if err != nil {
		log.WithError(err).Error("Error starting the ChromeDriver server")
		os.Exit(1)
	}

	caps := selenium.Capabilities{
		"browserName": "chrome",
	}

	wd, err := selenium.NewRemote(caps, "http://127.0.0.1:"+strconv.Itoa(port)+"/wd/hub")
	if err != nil {
		log.WithError(err).Error("error creating webdriver remote")
		os.Exit(1)
	}

	wd.Refresh()

	wd.Get("http://localhost:8000/")
	defer service.Stop()
}

func pickUnusedPort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		return 0, err
	}
	return port, nil
}
