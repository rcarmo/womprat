APP     := womprat
VERSION := 0.1.0
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: all clean windows linux darwin

all: windows

windows:
	GOOS=windows GOARCH=arm64 go build -ldflags="-H windowsgui $(LDFLAGS)" -o $(APP).exe .
	@echo "Built $(APP).exe (Windows ARM64, $(shell ls -lh $(APP).exe | awk '{print $$5}'))"

windows-amd64:
	GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui $(LDFLAGS)" -o $(APP)-amd64.exe .

linux:
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(APP)-linux .

darwin:
	GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(APP)-darwin .

clean:
	rm -f $(APP).exe $(APP)-amd64.exe $(APP)-linux $(APP)-darwin

deps:
	go mod tidy

dev:
	go run .

test:
	go vet ./...
