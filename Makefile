BINARY    := git-agent
LDFLAGS   := -ldflags="-s -w"
TRIMPATH  := -trimpath
BUILD_FLAGS := $(LDFLAGS) $(TRIMPATH)

.PHONY: build test clean install

build:
	go build $(BUILD_FLAGS) -o $(BINARY) .

test:
	go test -count=1 ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...

clean:
	rm -f $(BINARY)

install:
	go install $(BUILD_FLAGS) .
