GO ?= go
BIN_DIR ?= .
BUILD_FLAGS ?= -trimpath
LDFLAGS ?= -s -w
LINUX_GOARCH ?= amd64
BUILD_VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || printf '0.1.0-dev')
BUILD_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf 'unknown')
BUILD_DATETIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
VERSION_PACKAGE := github.com/eagle/proxystack-go/internal/version
VERSION_LDFLAGS := -X $(VERSION_PACKAGE).Version=$(BUILD_VERSION) -X $(VERSION_PACKAGE).Commit=$(BUILD_COMMIT) -X $(VERSION_PACKAGE).BuildDateTime=$(BUILD_DATETIME)
GO_LDFLAGS := $(LDFLAGS) $(VERSION_LDFLAGS)

PS_AGENT := $(BIN_DIR)/ps-agent
PS_SUB := $(BIN_DIR)/ps-sub

.PHONY: build build-linux

build: $(BIN_DIR)
	$(GO) build $(BUILD_FLAGS) -ldflags="$(GO_LDFLAGS)" -o $(PS_AGENT) ./cmd/ps-agent
	$(GO) build $(BUILD_FLAGS) -ldflags="$(GO_LDFLAGS)" -o $(PS_SUB) ./cmd/ps-sub

build-linux: $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_GOARCH) $(GO) build $(BUILD_FLAGS) -ldflags="$(GO_LDFLAGS)" -o $(PS_AGENT) ./cmd/ps-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_GOARCH) $(GO) build $(BUILD_FLAGS) -ldflags="$(GO_LDFLAGS)" -o $(PS_SUB) ./cmd/ps-sub

$(BIN_DIR):
	mkdir -p $(BIN_DIR)
