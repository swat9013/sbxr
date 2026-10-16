# 0016. host の skills を持ち込まないことを作る内容の項目にし、Sbx adapter は env 定義で skills の共有を切る

- Status: Accepted
- Date: 2026-10-16
- 覆す条件: 宣言で skills を VM へ持ち込みたい要望が出たとき。または、sbx の env 定義から `sandboxOptions.skills` が無くなったとき

## 決定

- 「host の skills を VM へ持ち込まない」を、Runtime が受け取る作る内容（[decision/0009](0009-runtime-receives-sandbox-spec.md)）の項目にする
- domain（`internal/sandbox`）は、作る内容を組み立てるたびに常にこの項目を頼む。宣言で切り替える形は作らない
- Sbx adapter は、頼まれたときだけ env 定義に `sandboxOptions.skills: "off"` を書く。頼まれなければ書かない（sbx の既定の共有になる）
- skills の共有の mode と、それを指す env 定義の key は sbx 固有の約束なので、Sbx adapter の内側に置く

## 根拠

- sbx は create のたびに host の skills store を VM の agent の skills ディレクトリ（`~/.claude/skills`）へ共有する。既定は `readonly`（または sbx 設定 `skills.defaultMode`）で、VM の `~/.claude/skills` に、共有 store（`/run/sbx/skills-shared`、virtiofs の ro mount）を指す symlink が並ぶ。host の個人 skill が宣言を通らずに VM の agent へ入り、agent runtime profile の「host 側の個人設定は持ち込まない」（CONTEXT.md）が崩れる。create の表示・read-back・drift のどれにも現れない（issue #83）
- 実測（2026-10-16、sbx v0.48.0）で、env 定義に skills の mode を指定する key があると確かめた
  - `sandboxOptions.skills: "off"` を書いた env 定義を `sbx env plan` が受け付け、plan に `sandboxOptions.skills: off` の行が出た
  - 対照として、`sandboxOptions` の下の未知の key は `unknown field "…" in sandboxOptions` で、top-level の `skills` は `unknown field "skills"` で拒まれた（key を黙って読み捨てたのではない）
  - この env 定義で `sbx env create --force` した VM には、`~/.claude/skills` が無く、`/proc/mounts` に skills の共有 store の mount が無かった
  - sbxr が書いた env 定義（`sandboxOptions.skills: "off"` と kit `sbxr-herdr`・`sbxr-boot`）で作った VM を `sbxr stop` で止め、`sbx exec` で起動し直した後も、`~/.claude/skills` は無く、共有 store の mount も無かった。`claude plugin list` に host の skill は出なかった
  - sbx 設定 `skills.defaultMode` を既定から変えた host での振る舞いは確かめていない
- 項目にして domain が頼むのは、VM に入るものを作る内容の項目として domain が頼み、adapter が実行基盤の仕組みで入れる形（decision/0009 の boot の再生・herdr の導入）にそろえるため
- env 定義で切ると、作成の時点で共有が入らない。VM の起動ごとに外す処理を kit に足さずに済む

## 却下した代替案

- adapter が自分の判断で常に `sandboxOptions.skills: "off"` を書く: decision/0009 が boot の再生で退けた形と同じで、VM に入るものを決める場所が domain と adapter に分かれる
- VM の `~/.claude/skills` から、共有 store を指す symlink を作成の後と起動ごと（kit の startup）に外す: env 定義の key で作成時に切れるので要らない。外すまでの間は共有が見え、kit の段が 1 つ増える
- 作成経路を `sbx create --skills off` に替える: 作成経路（ADR 0006 の `sbx env create`）の作り直しになる
- sbx の設定 `skills.defaultMode` を `off` にするよう利用者に求める: 利用者の他の sandbox まで変わる
- 宣言で skills を持ち込める形を作る: 要望が無い。出たら別の issue で扱う
