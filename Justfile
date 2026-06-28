coverage_threshold := "90"

# clone 直後や go.mod を変更した後の初期セットアップ。tidy と build を一括で実行する
default: setup

# clone 直後や go.mod を変更した後に実行して、依存解決とバイナリ生成をまとめて行う
setup: tidy build

# 手元でコードを書いた後に gofmt で全ファイルを一括整形する
fmt:
    gofmt -w $(find . -name '*.go')

# CI でフォーマット差分があれば検出して失敗させる。手元では fmt を使う
fmt-check:
    #!/bin/sh
    diff=$(gofmt -d $(find . -name '*.go'))
    if [ -n "$diff" ]; then
        echo "$diff"
        exit 1
    fi

# go vet でコンパイルは通るが問題になりやすいコードパターンを静的解析する
vet:
    go vet ./...

# 全パッケージのユニットテストを実行する。実装変更後の動作確認に使う
test:
    go test ./...

# カバレッジ付きでテストを実行し、全体カバレッジが閾値（90%）を下回れば失敗させる
coverage:
    #!/bin/sh
    go test -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
    go tool cover -func="{{justfile_directory()}}/coverage.out"
    go tool cover -func="{{justfile_directory()}}/coverage.out" | awk -v threshold="{{coverage_threshold}}" '/^total:/ { coverage=$3; sub(/%/, "", coverage); if (coverage + 0 < threshold + 0) { printf("coverage %.1f%% is below %.1f%%\n", coverage, threshold); exit 1 } printf("coverage %.1f%% meets %.1f%% threshold\n", coverage, threshold) }'

# 依存パッケージに既知の脆弱性がないかスキャンする。リリース前やセキュリティレビュー時に実行する
vuln:
    go tool govulncheck ./...

# go.mod と go.sum を整理する。依存を追加・削除した後や clone 直後に実行する
tidy:
    go mod tidy

# バイナリを ./bin/git-kura にビルドする。動作確認や手元での試用に使う
build:
    go build -o ./bin/git-kura ./cmd/git-kura

# バイナリをビルドしてからエンドツーエンドのウォークスルースクリプトを実行する。主要フローの結合確認に使う
walkthrough: build
    #!/bin/sh
    PATH="{{justfile_directory()}}/bin:$PATH" sh scripts/test/test-walkthrough.sh

# 依存パッケージのライセンスが許可リスト内に収まっているか確認する。新しい依存を追加したときに実行する
license-check:
    go tool go-licenses check --include_tests ./...

# サードパーティライセンス情報を third_party_licenses/ に書き出す。リリース成果物に同梱するために実行する
license-save:
    go tool go-licenses save ./cmd/git-kura --save_path third_party_licenses

# 指定バージョンのツールアーカイブを .tools-dist/ に生成する。リリース時に GitHub Actions から呼ばれる
tools-archive version:
    sh scripts/build-tools-archive.sh {{version}} .tools-dist

# golangci-lint でスタイルや品質ルールを検査する。PR 提出前や check の一部として実行する
lint:
    golangci-lint run

# CI パイプラインで実行するチェック群（フォーマット・vet・カバレッジ・脆弱性・ライセンス）をまとめて走らせる
ci: fmt-check vet coverage vuln license-check

# lint を加えたフルチェック。PR マージ前のローカル最終確認や pre-push フックとして使う
check: lint ci

# リリーススクリプトを実行する。引数にサブコマンドを渡して plan / validate / tag などのステップを制御する
release +subs:
    go run scripts/release/main.go {{subs}}
