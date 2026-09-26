# ユースケース

## 記述規約

- 1 ユースケース = 1 見出し。Primary Actor / Scope / Level / Trigger / 事前条件 / 成功保証 を箇条書きで固定する
- Main Success Scenario は番号付きリストで、1 ステップ 1 文・アクターを明記・意図を書く（コマンドの細部は書かない）。3〜9 ステップに収める
- Extensions は分岐点を `<step 番号><英字>`（例 `2a.`）で示し、内部ステップは `2a1.` 形式にする。復帰先を明記する
- Level は user-goal に固定する。単独の操作はユースケースにせず、MSS の 1 ステップとして書く
- Scope は全ユースケースで sbxr に固定する。利用者と外部のシステムは境界の外（[system.md](system.md)）
- 語は [CONTEXT.md](../../../CONTEXT.md) に揃える

| # | ユースケース | 主なコマンド |
|---|---|---|
| UC1 | 作られる内容を事前に確かめる | `sbxr plan` |
| UC2 | repo から sandbox VM を作る | `sbxr create` |
| UC3 | sandbox VM を止める | `sbxr stop` |
| UC4 | sandbox VM を撤去する | `sbxr destroy` |
| UC5 | 全 VM 共通の egress 許可を宣言に揃える | `sbxr policy sync` |
| UC6 | secret の値を登録する | `sbxr secret setup` |
| UC7 | 宣言の変更を反映する | plan → stop → destroy → create |

sandbox VM の再起動は sbxr の外（sbx exec・herdr の再接続）で行うので、ユースケースにしない。

## UC1 作られる内容を事前に確かめる

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、sandbox VM を作る前か宣言を変えた後に、何が作られるかを知りたい
- 事前条件: なし（sbx には問い合わせない）
- 成功保証: host の管理状態・sandbox VM・global rule のどれも変わっていない

### Main Success Scenario

1. 利用者が repo と投入方式を指定し、作られる内容を求める
2. sbxr が現在の宣言を確定する（git URL は一時ディレクトリへ clone し、default branch の HEAD の repo 宣言を読む。VM の cache clone には触れない）
3. sbxr が作られる内容を見せる（VM の環境変数、配線しない secret、落とした repo の egress、global rule の宛先、template を再利用するか作るか）
4. sbxr が、作成済みの VM があれば、作成時の宣言との drift を見せる

### Extensions

- 1a. git URL に copy か mount を指定した: sbxr は止まる
- 2a. 宣言が不正か、secret 要求に定義が無い: sbxr は理由を示して止まる
- 2b. origin を読めない: sbxr は警告し（VM の git で ssh 形を https に書き換えないだけ）、3 へ
- 4a. 作成済みの VM が無い（未作成・作成途中・別出所）: sbxr は drift を見せずに終える

## UC2 repo から sandbox VM を作る

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、repo で AI coding agent を動かす隔離環境を欲しい
- 事前条件: sbx が使える。herdr 連携が有効なら host に herdr がある
- 成功保証: sandbox VM が宣言どおりに構成されて稼働し、egress 自己検証が通り、作成時の宣言が状態ディレクトリに記録されている

### Main Success Scenario

1. 利用者が、repo と投入方式（既定は clone）を指定して作成を求める
2. sbxr が 3 スコープの宣言を merge し、作る内容を確定する（template を再利用するか作るかを含む）
3. sbxr が作る内容を見せ、利用者に承認を求める（確認関門）
4. 利用者が承認する
5. sbxr が cache 層の template を用意する（hash に合う template が無いか期限を過ぎていれば、build 用 VM で作って snapshot し、旧いものを消す）
6. sbxr が sandbox スコープの secret を置いてから template で sandbox VM を作り、repo を投入方式で渡し、作った後に sandbox スコープ rule を足す
7. sbxr が VM の中を宣言どおりにする（materialize → init → boot）
8. sbxr が VM 内から egress 自己検証を行う
9. sbxr が作成時の宣言を記録し、herdr 連携が有効なら herdr machine を登録する

### Extensions

- 1a. 作成済みの VM がある: sbxr は作成時の宣言との drift を報告する。差分が無ければ 0 で、あれば作り直す手順（UC7）を示して非 0 で終える
- 1b. 作成途中の VM がある: sbxr は destroy を促して止まる
- 1c. 同じ名前の VM が管理外か別出所: sbxr は触らずに止まる
- 1d. VM 消失（作成時の宣言はあるが VM が無い）: sbxr は destroy を促して止まる
- 1e. git URL に copy か mount を指定した: sbxr は止まる
- 2a. 宣言が不正か、secret 要求に定義が無い: sbxr は理由を示して止まる
- 2b. 要求された secret の注入先 host が egress で許可されていない: sbxr はその secret を配線から外し、作る内容に載せて 3 へ
- 2c. herdr 連携が有効なのに host に herdr が無い: sbxr は確認関門の前に止まる
- 3a. 投入方式が mount: sbxr は確認関門で、VM から host の作業ツリー（`.git` を含む）を書き換えられることと、ignored のファイル（`.env`、個人設定の `.claude/settings.local.json` など）も VM から見えることを示す
- 3b. `--yes`: sbxr は確認関門を省く。git URL なら repo の egress を落とし、落とした宛先を示して 5 へ
- 3c. 端末が無い: sbxr は止まる（`--yes` を案内する）
- 4a. 利用者が承認しない: sbxr は何も残さずに止まる（git URL の cache clone も消す）
- 5a. secret ファイルが読めないか、配線する secret の値が無い: sbxr は template も状態ディレクトリも作らずに止まる（build 用 VM にも同じ配線をするので、値は template より前に確かめる）
- 5b. build 用 VM の残骸がある: sbxr はそれを撤去してから 5 を続ける
- 5c. template の作成が失敗した: sbxr は build 用 VM を撤去し、本番の VM を作らずに止まる（何も残さない）
- 5d. 旧い template を消せない（使っている VM がある等）: sbxr は警告して 6 へ
- 6a. secret を置けないか、VM を作れない: sbxr は状態ディレクトリを残して止まる（UC4 で片付ける）
- 7a. VM の中の段（sandbox スコープ rule・kit の startup・materialize・init・boot）が失敗した: sbxr は VM を調べられるよう稼働したまま残し、stop → destroy → create の復旧手順を示して止まる
- 8a. 許可先に届かないか、許可外に届いた: 7a と同じく止まる。利用者は宣言か global rule を直してから作り直す
- 9a. herdr machine の登録に失敗した: VM は作成済みとして残り、sbxr は登録し直す手順を示して非 0 で終える
- 9b. 同じ `<名前>.sbx` の登録が既にある: sbxr はその登録に触れずに止まり、解除してから登録し直す手順を示す

## UC3 sandbox VM を止める

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、VM での作業を中断して資源を空けたい
- 事前条件: sbxr が作った VM（作成途中を含む）がある
- 成功保証: VM が止まり、herdr 連携の VM なら herdr machine が無効になっている（herdr が VM を起こし直さない）

### Main Success Scenario

1. 利用者が VM の停止を求める
2. sbxr が、sbxr の作った VM であることを確かめる
3. sbxr が herdr machine を無効にする
4. sbxr が VM を止める
5. sbxr が、起動し直した後に herdr machine を有効に戻すコマンドを示す

### Extensions

- 2a. VM が無いか、管理外か、別出所: sbxr は止まる
- 2b. VM 消失: sbxr は destroy を促して止まる
- 3a. herdr 連携を有効にせずに作った VM: 4 へ進み、5 を行わない
- 3b. host に herdr が無い: sbxr は VM に触れずに止まる
- 3c. herdr machine の登録が無い（利用者が手で解除した等）: sbxr は警告して 4 へ進み、5 を行わない
- 3d. herdr machine を無効にできない: sbxr は VM を止めずに止まる（止めても herdr が起こし直すため）
- 4a. VM を止められない: sbxr は herdr machine を有効に戻して止まる（VM は動いたままなので、herdr から見失わせない）

## UC4 sandbox VM を撤去する

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、VM での作業を終えたか、作り直したい
- 事前条件: sbxr が作った VM（作成途中・VM 消失を含む）の状態ディレクトリがある
- 成功保証: VM と、その sandbox スコープの secret・rule、herdr machine、cache clone、状態ディレクトリが無い。作成済みの clone・copy の VM は、未回収が無いことを確かめてから撤去されている

### Main Success Scenario

1. 利用者が VM の撤去を求める
2. sbxr が、sbxr の作った VM で、止まっていることを確かめる
3. sbxr が VM を一時起動し、未回収が無いことを確かめて、止め直す
4. sbxr が、VM 内の変更が失われることを示して承認を求める
5. 利用者が承認する
6. sbxr が herdr machine を解除する
7. sbxr が VM を、sandbox スコープの secret・rule ごと撤去する
8. sbxr が cache clone と状態ディレクトリを片付ける

### Extensions

- 2a. VM の状態ディレクトリが無いか、別出所: sbxr は止まる
- 2b. VM が稼働中（作成途中・稼働を含む）: sbxr は、stop してから撤去するか `--force` を付けるよう示して止まる
- 2c. `--force`: sbxr は稼働中でも進み、3 を省いて 4 へ
- 2d. herdr 連携を有効にして作った VM で、host に herdr が無い: sbxr は止まる
- 3a. 投入方式が mount・作成途中の VM・VM 消失: sbxr は検査せずに 4 へ（mount の作業は host の作業ツリーにあり、作成途中の VM は agent に渡していない）
- 3b. 未回収がある: sbxr は、branch ごとの未 push の commit・未 commit の変更・stash を示し、VM を止め直して止まる。利用者は VM 内から origin へ push して 1 からやり直すか、`--force` を付ける
- 3c. 一時起動か検査が失敗した（origin に届かない等）: sbxr は VM を止め直して止まる（`--force` で省ける）。origin の host の egress 許可か secret の配線が無い repo では、毎回ここで止まる
- 4a. `--yes`: sbxr は確認を省いて 6 へ
- 4b. 端末が無い: sbxr は止まる（`--yes` を案内する）
- 5a. 利用者が承認しない: sbxr は止まる
- 5b. 確認の間に VM が起動した: sbxr は 2b と同じく止まる
- 6a. herdr machine を解除できない: sbxr は解除の手順を警告して 7 へ進み、最後に非 0 で終える
- 7a. VM を消せない: sbxr は状態ディレクトリを残して止まる（撤去には env 定義が要るため）
- 8a. cache clone か状態ディレクトリを消せない: sbxr は警告し、非 0 で終える

## UC5 全 VM 共通の egress 許可を宣言に揃える

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、user 設定の egress 宣言を変えたか、global rule が宣言と揃っているかを知りたい
- 事前条件: sbx が使える
- 成功保証: sbxr が編集してよい global rule が、default と user 設定の egress 宣言の期待集合と一致している。sandbox スコープ rule と sbx 自身が管理する rule には触れていない

### Main Success Scenario

1. 利用者が、global rule を宣言に揃えるよう求める
2. sbxr が、default と user 設定の egress 宣言から期待集合を作る
3. sbxr が、global rule と期待集合の差分を出す
4. sbxr が、足りない宛先を足してから余分な rule を消す（宣言に残る宛先を途中で塞がない）
5. sbxr が、読み直して一致を確かめ、行った変更を見せる

### Extensions

- 1a. `--check`: sbxr は差分を見せるだけで変えない。差分があれば非 0 で終える
- 2a. 宣言が不正: sbxr は止まる
- 4a. 途中で失敗した: sbxr は、そこまでに行った変更を見せて止まる
- 5a. 読み直しても一致しない: sbxr は止まる

## UC6 secret の値を登録する

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、VM の agent に使わせる token を用意した
- 事前条件: 端末がある（値を画面に出さずに読む）
- 成功保証: secret ファイル（mode 0600）に、secret 定義の key で値が書かれている。GitHub の token は、能力の確認が通ったものだけが書かれている

### Main Success Scenario

1. 利用者が、GitHub の token の登録を求める
2. sbxr が、付ける権限と付けない権限（Workflows は API で確かめられないので、作るときに付けない）、token の作り方、repo の削除と force push は token では防げないので branch protection で守ることを示す
3. 利用者が、probe に使う private repo（commit が 1 つ以上あるもの）と token を渡す
4. sbxr が、GitHub API で token の能力を確かめる（Contents を読め、Secrets・Actions・Administration が拒否される）
5. sbxr が、secret ファイルへ token を書く

### Extensions

- 1a. 注入先 host を指定した登録（custom）: sbxr は、その host を注入先に持つ secret 定義の key を決め、能力を確かめないことを示して値を読み、5 へ
  - 1a1. host を注入先に持つ定義が無いか、key が 1 つに決まらない: sbxr は止まる
- 3a. repo の書式が違う: sbxr は token を尋ねる前に止まる
- 3b. 端末が無いか、値が空: sbxr は止まる
- 4a. 能力が違う（拒否されるべき権限が通る、Contents を読めない）: sbxr は書かずに止まる
- 5a. secret ファイルの mode が 0600 でない: sbxr は止まる

## UC7 宣言の変更を反映する

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、repo 宣言か user 設定を変えた
- 事前条件: 作成済みの VM がある
- 成功保証: VM が現在の宣言から作り直されている。clone・copy の VM の作業は origin に残っている

### Main Success Scenario

1. 利用者が、repo 宣言か user 設定を変える
2. 利用者が、作られる内容と drift を確かめる（UC1）
3. 利用者が、VM 内の作業を origin へ push する
4. 利用者が、VM を止めて撤去する（UC3 → UC4）
5. 利用者が、VM を作り直す（UC2）

### Extensions

- 2a. drift が無い: ここで終える
- 2b. 変えたのが user 設定の egress だけ（global rule）: 利用者は UC5 で揃える（既存の VM にも効く）。ここで終える
- 3a. 投入方式が mount: push は要らない（作業は host の作業ツリーにある）。4 へ
- 4a. 未回収が残っている: UC4 の 3b で止まる。3 へ戻る
- 5a. cache 層か inputs の内容が変わった: UC2 の 5 で新しい template を作り、旧いものを消す

却下: 作り直しを 1 コマンドにする（`sbxr recreate`）。理由: destroy を自動では実行しない方針（README の drift 節）を保ち、未回収をどう扱うかの判断を利用者の手に残す。
