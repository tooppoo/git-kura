---
name: git-kura
description: >
  git-kura リポジトリ自体を開発・レビューするときに使う worktree 管理 skill。
  「〇〇 issue / key を実装して」「〇〇 key をレビューして」という依頼を受けたとき、
  または worktree を切る・移動する・閉じる操作が必要なときに必ず参照すること。
  worktree path は推測せず、必ず git-kura コマンドで解決する。
---

# git-kura worktree skill (dogfood / maintainer 用)

このリポジトリ自体を `git-kura` で開発するための skill。
**worktree path を手で推測したり、`git worktree add` を直接使ったりしない。**
task key を source of truth として、すべての worktree 操作を `git kura` 経由で行う。

---

## 基本原則

- `git kura` が利用可能な前提で動く（未インストールなら `make build && cp ./bin/git-kura /usr/local/bin/git-kura` を案内する）
- worktree path は **必ず** `git kura get <key>` で解決する
- main checkout を task の作業場所として使わない
- 変更予定のファイルは編集前に **必ず** `git kura seal claim` で claim する
- `merge` / `rebase` / `push` / `close` は明示指示があるときだけ実行する

---

## ワークフロー 1：開発依頼を受けたとき

**トリガー**: 「issue N を実装して」「key X の機能を作って」など

このワークフローは必ず以下の順序で実行する。seal を飛ばして編集を始めない。

```
1. worktree を作る
   git kura open <key>
   # → branch と worktree を決定論的に作成

2. worktree に移動する
   cd "$(git kura get <key>)"

3. 作業開始前に worktree の状態を確認する
   git status --short
   # 未コミット変更や index の共有状態を確認する。
   # 前のセッションから想定外の変更が残っている場合は、
   # ユーザーに確認してから作業を進める。

4. 変更予定のファイルを claim する
   - まず変更予定のファイルを一覧にする
   - その全ファイルを claim する
     git kura seal claim <files...>
   # claim は path が repo root 相対・存在するファイルであることを要求する。
   # 新規作成予定のファイルは先に作成（例: touch）してから claim する。

5. seal が競合したら作業を中断する
   - claim が exit code 6（"seal-conflict:"）で失敗したら、
     競合したファイルと、それを claim している key を報告して作業を止める。
   - 自分で competing key を unclaim したり、強制的に編集を進めたりしない。

6. seal が競合しなかったら、実際に変更を行う
   - claim 済みのファイルだけを編集する。
   - 編集対象が増えたら、その都度 git kura seal claim で追加 claim する。

7. review を受けて、指示が出たら PR を作成する
   # review より前、または PR 作成指示が出るより前に push / PR を作らない。

8. マージ指示を受けたら、claim していたファイルを全て解放する
   git kura seal unclaim <files...>
   # claim した全ファイルを unclaim する。claim 状況は git kura seal ls <key> で確認できる。

9. worktree を離れる前に状態を確認する
   git status --short
   # 想定外の未コミット変更がないことを確認してから片付けに進む。

10. worktree を片付ける
    cd "$(git kura get <key> --root)"   # repo root に戻る
    git kura close <key>                 # worktree と branch を削除（safety check 後）
    git pull                             # main を更新
```

---

## ワークフロー 2：レビュー依頼を受けたとき

**トリガー**: 「key X をレビューして」「〇〇 の変更を確認して」など

```
1. key を確認する
   - 依頼文から key を特定する
   - 不明なら確認する

2. key が open 済みか確認する
   git kura ls
   # open されていなければ依頼者に確認し、必要なら git kura open <key>

3. worktree を解決して移動
   cd "$(git kura get <key>)"

4. レビュー開始
   - diff / log / test 結果を確認する
   - AI prompt 向けコンテキストが必要なら: git kura get <key> --toon
   - script 向け metadata が必要なら:     git kura get <key> --json

5. review 中に修正を依頼された場合だけ、変更前に git status --short を確認し対象ファイルを claim する
   git status --short
   git kura seal claim <files...>
   # seal の失敗時は開発ワークフローと同じルールに従い、回避せず止める。
```

---

## worktree 終了時の safety check

`git kura close <key>` を実行する前に **必ず** 以下を確認する。

```bash
cd "$(git kura get <key>)"
git status --short
```

以下のいずれかがある場合は、明示確認なしに close しない。

- uncommitted changes
- untracked files（意図的なものか確認）
- unpushed commits
- 未解決の merge / rebase 判断

---

## コマンドリファレンス（よく使うもの）

| 目的 | コマンド |
|------|---------|
| worktree を作成 | `git kura open <key>` |
| worktree path を解決 | `git kura get <key>` |
| branch 名を解決 | `git kura get <key> --branch` |
| AI prompt 向け context | `git kura get <key> --toon` |
| script 向け metadata | `git kura get <key> --json` |
| repo root を取得 | `git kura get <key> --root` |
| open 中の一覧 | `git kura ls` |
| ファイルを claim | `git kura seal claim <files...>` |
| claim を解放 | `git kura seal unclaim <files...>` |
| 競合を事前確認（read-only） | `git kura seal test <files...>` |
| claim 状況を確認 | `git kura seal ls [key]` |
| worktree を閉じる | `git kura close <key>` ※safety check 後 |
| dry-run で確認 | `git kura open <key> --dry-run` |

exit code: 0=成功 / 1=一般エラー / 2=使い方エラー / 3=unsafe拒否 / 4=not found / 5=seal lock timeout / 6=seal-conflict / 7=seal-doctor-error

---

## Claude Code との関係

Claude Code の `--worktree` mode と git-kura 管理 worktree を **混同しない**。

推奨運用:
```bash
git kura open <key>
cd "$(git kura get <key>)"
claude          # git-kura が作成した worktree の中で Claude Code を起動
```

Claude Code に worktree を作らせるのではなく、git-kura が解決した worktree の中で起動する。
