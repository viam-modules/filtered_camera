
GO_BUILD_ENV :=
GO_BUILD_FLAGS := -tags no_cgo,osusergo,netgo
MODULE_BINARY := filtered-camera

ifeq ($(VIAM_TARGET_OS), windows)
	GO_BUILD_ENV += GOOS=windows GOARCH=amd64
	MODULE_BINARY = filtered-camera.exe
endif

ifeq ($(VIAM_TARGET_OS),linux)
    GO_BUILD_FLAGS += -ldflags="-extldflags=-static -s -w"
endif

$(MODULE_BINARY): Makefile *.go */*.go cmd/module/*.go
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(MODULE_BINARY) cmd/module/cmd.go

module.tar.gz: meta.json $(MODULE_BINARY)
	tar czf $@ meta.json $(MODULE_BINARY) 
	git checkout meta.json

ifeq ($(VIAM_TARGET_OS), windows)
module.tar.gz: fix-meta-for-win
else
module.tar.gz: strip-module
endif

strip-module: 
	strip filtered-camera

# TODO: Remove when viamrobotics/rdk#4969 is deployed
fix-meta-for-win:
	jq '.entrypoint = "filtered-camera.exe"' meta.json > temp.json && mv temp.json meta.json

test:
	go test ./...

lint:
	gofmt -w .
	go run github.com/rhysd/actionlint/cmd/actionlint@latest

all: module test

update:
	go get go.viam.com/rdk@latest
	go mod tidy
