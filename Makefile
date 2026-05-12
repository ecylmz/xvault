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

.PHONY: dist
dist:
	rm -rf dist
	mkdir -p dist
	for os in linux darwin; do \
		for arch in amd64 arm64; do \
			name=xvault-$$os-$$arch; \
			mkdir -p dist/$$name; \
			GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$$name/xvault ./cmd/xvault; \
			cp README.md LICENSE CHANGELOG.md dist/$$name/; \
			(cd dist && tar -czf $$name.tar.gz $$name && shasum -a 256 $$name.tar.gz > $$name.tar.gz.sha256); \
		done; \
	done

.PHONY: release-check
release-check:
	sh tools/check_release_safety.sh

.PHONY: verify-archive
verify-archive:
	go build -o bin/xvault ./cmd/xvault
	bin/xvault verify-archive --json

.PHONY: docker-check
docker-check:
	sh tools/docker_check.sh

.PHONY: publish-check
publish-check: ci lint build cross-build verify-archive

.PHONY: release
release:
	sh tools/release.sh "$(VERSION)"

.PHONY: update-golden
update-golden:
	UPDATE_GOLDEN=1 go test ./...
