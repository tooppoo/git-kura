# 検査系コマンドの構造化出力では実行可否と検査結果を分離する

- Status: Accepted
- Created: 2026-06-19T15:00:00Z

## Context

git-kura は output framework により、JSON output を共通 envelope で出力する（[output framework ADR](20260617T070134Z_output-framework-envelope-result-renderer.md) 参照）。

`seal test` / `seal doctor` のような read-only diagnostic command では、以下の2つが別概念になる。

- コマンド実行そのものが可能だったか
- 検査対象が条件を満たしたか / 健全だったか

これらを混同すると、JSON consumer は `ok: false` が「コマンドを実行できなかった」のか、「検査はできたが不合格だった」のかを判別しにくくなる。

issue #71 では `seal test` と `seal doctor` を output framework に移行する際に、この区別を明示的に設計に組み込んだ。

## Decision

Diagnostic command の structured output では、共通 envelope の `ok` はコマンド実行の成否を表す。

検査対象の結果は command-specific data に置く。

```
検査自体を実行できない:
  ok: false
  error: {...}

検査は実行できたが不合格 / unhealthy:
  ok: true
  data.passed: false   (seal test)
  または
  data.healthy: false  (seal doctor)
```

### `seal test` への適用

`seal test` では current key を解決して path の seal 状態を判定する。

- current key 解決失敗（not-inside-git-repository / not-in-managed-worktree / metadata-missing / metadata-inconsistent）: `ok: false`
- invalid path（absolute / repository 外 / 正規化不能）: `ok: false`
- other key に claim された path がある: `ok: true`, `data.passed: false`
- missing path（repository 内の未作成 path）: `ok: true`, business result として `data.results[]` に載せる

`missing-path` は未作成 path を表す business result であり、入力契約違反ではない。`safe: true` として扱う。

absolute path / repository 外 path / 正規化不能 path は診断対象として構築できない入力契約違反なので、business result にはしない。

### `seal doctor` への適用

`seal doctor` は repository-wide inspection として扱う。current key には依存しない。

- malformed store（store file 読み取り不能 / JSON parse 失敗 / schema validation 失敗）: `ok: false`, `error.code: seal-doctor-error`
- store は読めたが integrity violation がある: `ok: true`, `data.healthy: false`, `data.findings[]`

JSON consumer は `ok` で doctor 実行可否を、`data.healthy` で store の健全性を判定する。

### reason token の整合

`error.code` は stderr reason token と整合させる。

JSON 専用の error code は原則作らない。

`seal test` の current key 解決失敗時の `current-key-unresolved` は正式な reason token として扱う。

- 非 JSON 時の stderr も `current-key-unresolved` を含む
- JSON 時の `error.code` も `current-key-unresolved`
- `error.details.reason` は詳細分類として、`not-inside-git-repository` / `not-in-managed-worktree` / `metadata-missing` / `metadata-inconsistent` を持つ

### exit code の維持

`--json` 指定時も exit code は既存契約を維持する。

- `seal test` の conflict（`data.passed: false`）は JSON envelope が `ok: true` であっても exit code 6 を使う
- `seal doctor` の malformed store は exit code 7 を使う（`ok: false`）
- `seal doctor` の integrity violation は exit code 7 を使う（`ok: true` だが `data.healthy: false`）

JSON consumer は `ok` だけでなく、command-specific data も読む必要がある。

## Alternatives Considered

### `ok: false` で conflict / unhealthy を表現する

`seal test` の conflict や `seal doctor` の integrity violation を `ok: false` で表現すると、「コマンドが実行できなかった」と「検査対象が条件を満たさなかった」の区別がなくなる。JSON consumer はエラーの種類を `error.code` から判断しなければならず、実行失敗と業務的不合格の判定軸が混在する。

### exit code を変更して JSON / 非 JSON で揃える

`ok: true` + `data.passed: false` + exit 0 にすると、既存 CLI スクリプトとの互換性が失われる。exit code は安定した出力契約の一部であるため、既存値を維持する。

## Output Contract

- `ok` は command execution の成否を表す stable contract field。
- `data.passed` / `data.healthy` は command-specific な検査結果 field。
- exit code は `--json` 指定時も既存契約を維持する。
- `error.code` は stderr reason token と整合する hyphen-case token。
- `current-key-unresolved` は `seal test` の current key 解決失敗に使う正式な reason token。

## Consequences

### Positive Consequences

- `ok: false` の意味が明確になる（実行できなかった、ではなく実行が失敗した）。
- diagnostic result を machine-readable に扱いやすくなる。
- command-specific schema の責務が明確になる。
- human output と JSON output の乖離を抑えられる（`error.code` と stderr reason token が揃う）。

### Negative Consequences

- `exit code != 0` でも JSON envelope は `ok: true` になり得る。
- JSON consumer は `ok` だけでなく、command-specific data も読む必要がある。
- command-specific schema と代表 fixture による検証が重要になる。

### Neutral Consequences

- `seal test` の business result（passed/failed）と実行可否（ok）は独立して変化し得る。
- 将来 `tools doctor` や `config doctor` のような検査系コマンドを追加しても同じ原則を使える。
