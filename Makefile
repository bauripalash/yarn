-include environ.inc
.PHONY: deps dev build install image release test clean tr tr-merge

export CGO_ENABLED=0
VERSION=$(shell git describe --abbrev=0 --tags 2>/dev/null || echo "$VERSION")
COMMIT=$(shell git rev-parse --short HEAD || echo "$COMMIT")
BRANCH=$(shell git rev-parse --abbrev-ref HEAD)
GOCMD=go
GOVER=$(shell go version | grep -o -E 'go1\.17\.[0-9]+')

DESTDIR=/usr/local/bin

ifeq ($(BRANCH), main)
IMAGE := prologic/yarnd
TAG := latest
else
IMAGE := prologic/yarnd
TAG := dev
endif

all: preflight build

preflight:
	@./preflight.sh

deps:
	@$(GOCMD) install github.com/tdewolff/minify/v2/cmd/minify@latest
	@$(GOCMD) install github.com/nicksnyder/go-i18n/v2/goi18n@latest
	@$(GOCMD) install github.com/astaxie/bat@latest

dev : DEBUG=1
dev : build
	@./yarnc -v
	@./yarnd -D -O -R $(FLAGS)

cli:
	@$(GOCMD) build -tags "netgo static_build" -installsuffix netgo \
		-ldflags "-w \
		-X $(shell go list).Version=$(VERSION) \
		-X $(shell go list).Commit=$(COMMIT)" \
		./cmd/yarnc/

server: generate
	@$(GOCMD) build $(FLAGS) -tags "netgo static_build" -installsuffix netgo \
		-ldflags "-w \
		-X $(shell go list).Version=$(VERSION) \
		-X $(shell go list).Commit=$(COMMIT)" \
		./cmd/yarnd/...

build: cli server

generate:
	@if [ x"$(DEBUG)" = x"1"  ]; then		\
	  echo 'Running in debug mode...';	\
	else								\
	  minify -b -o ./internal/theme/static/css/yarn.min.css ./internal/theme/static/css/[0-9]*-*.css;	\
	  minify -b -o ./internal/theme/static/js/yarn.min.js ./internal/theme/static/js/[0-9]*-*.js;		\
	fi

install: build
	@install -D -m 755 yarnd $(DESTDIR)/yarnd
	@install -D -m 755 yarnc $(DESTDIR)/yarnc

ifeq ($(PUBLISH), 1)
image: generate
	@docker build --build-arg VERSION="$(VERSION)" --build-arg COMMIT="$(COMMIT)" -t $(IMAGE):$(TAG) .
	@docker push $(IMAGE):$(TAG)
else
image: generate
	@docker build --build-arg VERSION="$(VERSION)" --build-arg COMMIT="$(COMMIT)" -t $(IMAGE):$(TAG) .
endif

release: generate
	@./tools/release.sh

fmt:
	@$(GOCMD) fmt ./...

test:
	@CGO_ENABLED=1 $(GOCMD) test -v -cover -race ./...

coverage:
	@CGO_ENABLED=1 $(GOCMD) test -v -cover -race -cover -coverprofile=coverage.out  ./...
	@$(GOCMD) tool cover -html=coverage.out

bench: bench-yarn.txt
	go test -race -benchtime=1x -cpu 16 -benchmem -bench "^(Benchmark)" go.yarn.social/types

bench-yarn.txt:
	curl -s https://twtxt.net/user/prologic/twtxt.txt > $@

clean:
	@git clean -f -d -X

tr:
	@goi18n merge -outdir ./internal/langs ./internal/langs/active.*.toml

tr-merge:
	@goi18n merge -outdir ./internal/langs ./internal/langs/active.*.toml ./internal/langs/translate.*.toml 
