GO ?= go
BIN_DIR ?= .
BUILD_FLAGS ?= -trimpath
LDFLAGS ?= -s -w
LINUX_GOARCH ?= amd64

PS_AGENT := $(BIN_DIR)/ps-agent
PS_SUB := $(BIN_DIR)/ps-sub

.PHONY: build build-linux

build: $(BIN_DIR)
	$(GO) build $(BUILD_FLAGS) -ldflags="$(LDFLAGS)" -o $(PS_AGENT) ./cmd/ps-agent
	$(GO) build $(BUILD_FLAGS) -ldflags="$(LDFLAGS)" -o $(PS_SUB) ./cmd/ps-sub

build-linux: $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_GOARCH) $(GO) build $(BUILD_FLAGS) -ldflags="$(LDFLAGS)" -o $(PS_AGENT) ./cmd/ps-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=$(LINUX_GOARCH) $(GO) build $(BUILD_FLAGS) -ldflags="$(LDFLAGS)" -o $(PS_SUB) ./cmd/ps-sub

$(BIN_DIR):
	mkdir -p $(BIN_DIR)
