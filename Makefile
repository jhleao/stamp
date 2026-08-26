VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
GOOGLE_OAUTH_CLIENT_ID ?=
GOOGLE_OAUTH_CLIENT_SECRET ?=
GOOGLE_PICKER_API_KEY ?=
GO_LDFLAGS := -X main.version=$(VERSION)
ifneq ($(strip $(GOOGLE_OAUTH_CLIENT_ID)),)
GO_LDFLAGS += -X github.com/jhleao/stamp/internal/drive.DefaultOAuthClientID=$(GOOGLE_OAUTH_CLIENT_ID)
endif
ifneq ($(strip $(GOOGLE_OAUTH_CLIENT_SECRET)),)
GO_LDFLAGS += -X github.com/jhleao/stamp/internal/drive.DefaultOAuthClientSecret=$(GOOGLE_OAUTH_CLIENT_SECRET)
endif
ifneq ($(strip $(GOOGLE_PICKER_API_KEY)),)
GO_LDFLAGS += -X github.com/jhleao/stamp/internal/drive.DefaultPickerAPIKey=$(GOOGLE_PICKER_API_KEY)
endif

.PHONY: frontend build test smoke install

frontend:
	npm run build:studio

build: frontend
	mkdir -p bin
	go build -ldflags "$(GO_LDFLAGS)" -o bin/stamp ./cmd/stamp

test: frontend
	npm run test:studio
	go test ./...

smoke: build
	./scripts/smoke.sh

install: build
	mkdir -p "$(BINDIR)"
	install -m 0755 bin/stamp "$(BINDIR)/stamp"
