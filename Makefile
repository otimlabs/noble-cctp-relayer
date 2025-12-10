VERSION := $(shell echo $(shell git describe --tags 2>/dev/null || echo "dev") | sed 's/^v//')
COMMIT  := $(shell git log -1 --format='%H')
DIRTY := $(shell git status --porcelain | wc -l | xargs)

ldflags = -X github.com/strangelove-ventures/noble-cctp-relayer/cmd.Version=$(VERSION) \
				-X github.com/strangelove-ventures/noble-cctp-relayer/cmd.Commit=$(COMMIT) \
				-X github.com/strangelove-ventures/noble-cctp-relayer/cmd.Dirty=$(DIRTY)

ldflags += $(LDFLAGS)
ldflags := $(strip $(ldflags))

# used for Docker build
GOPATH := $(shell go env GOPATH)
GOBIN := $(GOPATH)/bin


###############################################################################
###                          Formatting & Linting                           ###
###############################################################################
.PHONY: lint lint-fix test

golangci_lint_cmd=golangci-lint
golangci_version=v1.57.2

lint:
	@echo "🔍 Running linter"
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(golangci_version)
	@$(GOBIN)/$(golangci_lint_cmd) run ./... --timeout 15m

lint-fix:
	@echo "🔧 Running linter and fixing issues"
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(golangci_version)
	@$(GOBIN)/$(golangci_lint_cmd) run ./... --fix --timeout 15m

test:
	@echo "🧪 Running tests"
	@go test -v ./circle ./cmd ./types ./ethereum -skip 'TestV1Attestation|TestToMessageStateSuccess|TestStartListener'


###############################################################################
###                              Install                                    ###
###############################################################################
.PHONY: install

install: go.sum
	@echo "🤖 Building noble-cctp-relayer..."
	@go build -mod=readonly -ldflags '$(ldflags)' -o $(GOBIN)/noble-cctp-relayer main.go

###############################################################################
###                              Docker                                     ###
###############################################################################
.PHONEY: local-docker

local-docker:
	@echo "🤖 Building docker image noble-cctp-relayer:local"
	@docker build -t cctp-relayer:local-test -f ./local.Dockerfile .
