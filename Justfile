coverage_threshold := "90"

default: setup

setup: tidy build

fmt:
    gofmt -w $(find . -name '*.go')

fmt-check:
    #!/bin/sh
    diff=$(gofmt -d $(find . -name '*.go'))
    if [ -n "$diff" ]; then
        echo "$diff"
        exit 1
    fi

vet:
    go vet ./...

test:
    go test ./...

coverage:
    #!/bin/sh
    go test -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
    go tool cover -func="{{justfile_directory()}}/coverage.out"
    go tool cover -func="{{justfile_directory()}}/coverage.out" | awk -v threshold="{{coverage_threshold}}" '/^total:/ { coverage=$3; sub(/%/, "", coverage); if (coverage + 0 < threshold + 0) { printf("coverage %.1f%% is below %.1f%%\n", coverage, threshold); exit 1 } printf("coverage %.1f%% meets %.1f%% threshold\n", coverage, threshold) }'

vuln:
    go tool govulncheck ./...

tidy:
    go mod tidy

build:
    go build -o ./bin/git-kura ./cmd/git-kura

walkthrough: build
    #!/bin/sh
    PATH="{{justfile_directory()}}/bin:$PATH" sh scripts/test/test-walkthrough.sh

license-check:
    go tool go-licenses check --include_tests ./...

license-save:
    go tool go-licenses save ./cmd/git-kura --save_path third_party_licenses

tools-archive version:
    sh scripts/build-tools-archive.sh {{version}} .tools-dist

lint:
    golangci-lint run

ci: fmt-check vet coverage vuln license-check

check: lint ci

release +subs:
    go run scripts/release/main.go {{subs}}
