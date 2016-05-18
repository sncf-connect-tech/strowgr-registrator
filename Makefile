.PHONY: all test clean build install dist

BUILDDIR=bin
BINARY=registrator
REGISTRY="dockerregistrydev.socrate.vsct.fr/"
IMAGE=$(REGISTRY)strowgr/$(BINARY)
COMMIT_ID=$(shell git rev-parse --short HEAD)
VERSION?=$(shell cat VERSION)

GOFLAGS ?= $(GOFLAGS:) -a -installsuffix cgo

all: docker-build docker-image

deps:
	go get -t ./...

generate:
	sed "s/{{ VERSION }}/$(VERSION)/" version.go.tpl > version.go

build: generate
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o ${BUILDDIR}/${BINARY}-linux_amd64 cmd/registrator.go

test: generate
	go test -v ./...

docker-build:
	docker build -t $(IMAGE)-builder -f Dockerfile.build .
	docker run -v $(CURDIR)/bin:/go/src/github.com/voyages-sncf-technologies/strowgr/registrator/bin $(IMAGE)-builder make build VERSION=$(VERSION)-$(COMMIT_ID)

docker-test:
	docker build -t $(IMAGE)-builder -f Dockerfile.build .
	docker run -v $(CURDIR)/bin:/go/src/github.com/voyages-sncf-technologies/strowgr/registrator/bin $(IMAGE)-builder make test

docker-image: dist
	cp Dockerfile dist
	docker build -t $(IMAGE):$(VERSION) dist

dist: ${BUILDDIR}/${BINARY}-linux_amd64
	rm -fr dist && mkdir dist
	cp ${BUILDDIR}/${BINARY}-linux_amd64 dist

clean:
	rm -fr {dist,bin}

shell:
	docker run --rm -ti $(IMAGE):$(VERSION) /bin/sh

run:
	docker rm -f $(BINARY); docker run --privileged -v /var/run/docker.sock:/var/run/docker.sock -d --name $(BINARY) $(IMAGE):$(VERSION) -url http://192.168.99.100:8080 -address 192.168.99.100 -verbose

logs:
	docker logs -f $(BINARY)

push:
	docker push $(IMAGE):$(VERSION)
