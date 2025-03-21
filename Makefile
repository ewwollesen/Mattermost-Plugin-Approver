.PHONY: build clean

GOPATH ?= $(shell go env GOPATH)
GO ?= $(shell command -v go 2> /dev/null)
MANIFEST_FILE ?= plugin.json

build:
	mkdir -p server/dist
	cd server && env GOOS=linux GOARCH=amd64 $(GO) build -o dist/plugin-linux-amd64
	cd server && env GOOS=darwin GOARCH=amd64 $(GO) build -o dist/plugin-darwin-amd64
	cd server && env GOOS=windows GOARCH=amd64 $(GO) build -o dist/plugin-windows-amd64.exe

clean:
	rm -rf server/dist

package: build
	mkdir -p dist
	cp $(MANIFEST_FILE) dist/
	cd server && cp -r dist ../dist/
	cd dist && zip -r approver-plugin.zip .

.DEFAULT_GOAL := build
