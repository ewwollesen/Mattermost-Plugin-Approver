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
	rm -rf dist
	mkdir -p dist
	cp $(MANIFEST_FILE) dist/
	mkdir -p dist/server/dist
	cp server/dist/* dist/server/dist/
	cd dist && tar -czf ../approver-plugin.tar.gz .

.DEFAULT_GOAL := build
