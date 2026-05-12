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

.PHONY: fmt-check
fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

.PHONY: ci
ci: fmt-check test release-check

.PHONY: build
build:
	go build -o bin/xvault ./cmd/xvault

.PHONY: cross-build
cross-build:
	for os in linux darwin; do \
		for arch in amd64 arm64; do \
			GOOS=$$os GOARCH=$$arch go build -o /tmp/xvault-$$os-$$arch ./cmd/xvault; \
		done; \
	done

.PHONY: release-check
release-check:
	sh tools/check_release_safety.sh

.PHONY: verify-archive
verify-archive:
	go build -o bin/xvault ./cmd/xvault
	bin/xvault verify-archive --json

.PHONY: publish-check
publish-check: ci lint build cross-build verify-archive

.PHONY: update-golden
update-golden:
	UPDATE_GOLDEN=1 go test ./...
