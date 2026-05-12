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
ci: fmt test release-check

.PHONY: build
build:
	go build -o bin/xvault ./cmd/xvault

.PHONY: release-check
release-check:
	sh tools/check_release_safety.sh

.PHONY: verify-archive
verify-archive:
	go build -o bin/xvault ./cmd/xvault
	bin/xvault verify-archive --json

.PHONY: update-golden
update-golden:
	UPDATE_GOLDEN=1 go test ./...
