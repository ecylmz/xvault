.PHONY: test
test:
	go test ./...

.PHONY: race
race:
	go test -race ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: ci
ci: fmt test

.PHONY: build
build:
	go build -o bin/xvault ./cmd/xvault

.PHONY: update-golden
update-golden:
	UPDATE_GOLDEN=1 go test ./...
