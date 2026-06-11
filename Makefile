BINARY := yaz2health

.PHONY: build test vet tidy install clean

build:
	go build -o bin/$(BINARY) ./cmd/$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

install:
	go install ./cmd/$(BINARY)

clean:
	rm -rf bin
