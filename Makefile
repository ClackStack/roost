VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: build test race e2e fuzz release offline-bundle clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o roost ./cmd/roost

test:
	go vet ./...
	go test ./...

race:
	go test -race ./...

# Needs strace, curl, openssl, and python3.
e2e:
	scripts/e2e.sh

# -fuzzminimizetime keeps the fuzzer from stalling on large seed files.
fuzz:
	go test ./internal/extract -run='^$$' -fuzz=FuzzExtract -fuzztime=5m -fuzzminimizetime=5x

release:
	rm -rf dist && mkdir dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/roost-$(VERSION)-$$os-$$arch$$ext ./cmd/roost || exit 1; \
	done
	cd dist && sha256sum roost-* > SHA256SUMS

# Source plus all dependencies, for building on a network with no module proxy:
#   tar xzf roost-*-src.tar.gz && cd roost && go build -mod=vendor ./cmd/roost
offline-bundle:
	mkdir -p dist
	go mod vendor
	tar czf dist/roost-$(VERSION)-src.tar.gz --exclude=dist --exclude=.git --transform 's,^\.,roost,' .
	rm -rf vendor

clean:
	rm -rf roost dist vendor
