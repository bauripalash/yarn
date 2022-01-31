package internal

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/gavv/httpexpect/v2"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var (
	bind string
	data string
)

func makeURL(partialURL string, args ...interface{}) string {
	if len(args) > 0 {
		partialURL = fmt.Sprintf(partialURL, args...)
	}
	return fmt.Sprintf("http://%s%s", bind, partialURL)
}

func TestInfo(t *testing.T) {
	e := httpexpect.New(t, makeURL(""))

	e.GET("/info").
		Expect().
		Status(http.StatusOK).
		Body().Contains("yarnd")
}

func TestMain(m *testing.M) {
	testDir, err := os.MkdirTemp("", "*-yarn-e2e-test")
	if err != nil {
		log.WithError(err).Error("error creating temporary test directory")
		os.Exit(-1)
	}
	data = testDir
	defer os.RemoveAll(testDir)

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		log.WithError(err).Error("error finding free test port")
		os.Exit(-1)
	}
	bind = listener.Addr().String()

	testStore := fmt.Sprintf("bitcask://%s/yarn.db", testDir)

	server, err := NewServer(
		bind,
		WithData(testDir),
		WithBaseURL(makeURL("")),
		WithStore(testStore),
		WithCookieSecret(GenerateRandomToken()),
		WithMagicLinkSecret(GenerateRandomToken()),
		WithAPISigningKey(GenerateRandomToken()),
	)
	if err != nil {
		log.WithError(err).Error("error starting test server")
		os.Exit(-1)
	}

	var eg errgroup.Group

	eg.Go(func() error {
		return server.Run()
	})

	os.Exit(m.Run())

	server.Shutdown(context.Background())

	if err := eg.Wait(); err != nil {
		log.WithError(err).Error("error running test server")
	}
}
