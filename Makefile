VERSION ?= 0.12.0-universal.3
BINARY := harnest
LDFLAGS := -s -w -X main.version=$(VERSION)
.PHONY: build clean release version

version:
	@echo $(VERSION)

build:
	mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY) ./cmd/harnest/

clean:
	rm -f bin/$(BINARY)
	rm -rf dist/

release: clean
	mkdir -p dist
	GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 ./cmd/harnest/
	GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 ./cmd/harnest/
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 ./cmd/harnest/
	GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 ./cmd/harnest/
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe ./cmd/harnest/
	GOOS=windows GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-windows-arm64.exe ./cmd/harnest/
	cd dist && (shasum -a 256 * > checksums.txt 2>/dev/null || sha256sum * > checksums.txt)
	@echo "Release binaries in dist/"
