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
| UC7 | 宣言の変更を反映する | `sbxr plan` → `sbxr recreate` |
| UC8 | 設定と host の前提を診断する | `sbxr doctor` |
| UC9 | AI に設定を書かせる | skill `sbxr-config` → `sbxr doctor` |
| UC10 | 止めた sandbox VM を起こし直す | `sbxr start` |

sbxr の外での起動（sbx exec・herdr の再接続）は、ユースケースにしない。状態機械の外部起動として扱う。

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

- 1a. git URL に copy を指定した: sbxr は止まる
- 2a. 宣言が不正か、secret 要求に定義が無い: sbxr は理由を示して止まる
- 2b. origin を読めない: sbxr は警告し（VM の git で ssh 形を https に書き換えないだけ）、3 へ
- 4a. 作成済みの VM が無い（未作成・作成途中・別出所）: sbxr は drift を見せずに終える

## UC2 repo から sandbox VM を作る

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、repo で AI coding agent を動かす隔離環境を欲しい
- 事前条件: sbx が使える。herdr 連携が有効なら host に herdr がある
- 成功保証: sandbox VM が宣言どおりに構成されて稼働し、egress 自己検証が通り、作成時の宣言が状態ディレクトリに記録されている。VM の中で agent（Claude Code）を起動でき、Docker を使える。herdr 連携の VM では、herdr の pane で agent を起動できる（decision/0027）。宣言の run が VM の中で始まっている（decision/0028）

### Main Success Scenario

1. 利用者が、repo と投入方式（既定は clone）を指定して作成を求める
2. sbxr が 3 スコープの宣言を merge し、作る内容を確定する（template を再利用するか作るかを含む）
3. sbxr が作る内容を見せ、利用者に承認を求める（確認プロンプト）
4. 利用者が承認する
5. sbxr が cache 層の template を用意する（hash に合う template が無いか期限を過ぎていれば、build 用 VM で作って snapshot し、旧いものを消す）
6. sbxr が sandbox スコープの secret を置いてから template で sandbox VM を作り、repo を投入方式で渡し、作った後に sandbox スコープ rule を足す
7. sbxr が VM の中を宣言どおりにする（materialize → init → boot）
8. sbxr が VM 内から egress 自己検証を行い、VM の中で agent と Docker が使えることを確かめる
9. sbxr が作成時の宣言を記録し、herdr 連携が有効なら herdr machine を登録して、VM 内の herdr の最初の workspace を VM 内の作業ツリーで開き直す（decision/0015）。最後に、宣言の run を VM 内の作業ツリーで裏で始め、始めたことと、出力が残る VM 内の log の場所を示す（decision/0028）

### Extensions

- 1a. 作成済みの VM がある: sbxr は作成時の宣言との drift を報告する。差分が無ければ 0 で、あれば作り直す手順（UC7）を示して非 0 で終える
- 1b. 作成途中の VM がある: sbxr は destroy を促して止まる
- 1c. 同じ名前の VM が管理外か別出所: sbxr は触らずに止まる
- 1d. VM 消失（作成時の宣言はあるが VM が無い）: sbxr は destroy を促して止まる
- 1e. git URL に copy を指定した: sbxr は止まる
- 1f. `--attach` を付けたのに端末が無い: sbxr は止まる
- 2a. 宣言が不正か、secret 要求に定義が無い: sbxr は理由を示して止まる
- 2b. 要求された secret の注入先 host が egress で許可されていない: sbxr はその secret を配線から外し、作る内容に載せて 3 へ
- 2c. herdr 連携が有効なのに host に herdr が無い: sbxr は確認プロンプトの前に止まる
- 3b. `--yes`: sbxr は確認プロンプトを省く。git URL なら repo の egress を落とし、落とした宛先を示して 5 へ
- 3c. 端末が無い: sbxr は止まる（`--yes` を案内する）
- 4a. 利用者が承認しない: sbxr は何も残さずに止まる（git URL の cache clone も消す）
- 5a. secret ファイルが読めないか、配線する secret の値が無い: sbxr は template も状態ディレクトリも作らずに止まる（build 用 VM にも同じ配線をするので、値は template より前に確かめる）
- 5b. build 用 VM の残骸がある: sbxr はそれを撤去してから 5 を続ける
- 5c. template の作成が失敗した: sbxr は build 用 VM を撤去し、本番の VM を作らずに止まる（何も残さない）
- 5d. 旧い template を消せない（使っている VM がある等）: sbxr は警告して 6 へ
- 6a. 状態ディレクトリに出所を記録する前（実行基盤の定義か作成の最初の記録を書けない。herdr 連携を有効にして、kit へ渡せない path の repo を作ろうとした場合を含む。decision/0015）に失敗した: sbxr は書きかけの状態ディレクトリと git URL の cache clone を消し、未作成に戻して止まる（sbx の側にはまだ何も置いていない。decision/0011）
- 6b. secret を置けないか、VM を作れない: sbxr は状態ディレクトリを残して止まる（UC4 で片付ける）
- 7a. VM の中の段（sandbox スコープ rule・kit の startup・materialize・init・boot）が失敗した: sbxr は VM を調べられるよう稼働したまま残し、復旧手順（`sbxr recreate`）を示して止まる
- 8a. 許可先に届かないか、許可外に届いたか、許可外への通信が proxy の拒否応答を得ずに失敗した: 7a と同じく止まる。利用者は宣言か global rule を直してから作り直す。「届かない」は proxy の拒否応答（403 と、body の 1 行目の `Approval required for <宛先>.` か `Blocked by local rule for <宛先>`）のことで、それ以外の HTTP 応答は状態コードを問わず「届いた」とみなす（decision/0007・0016）
- 8b. 許可集合に、glob を含まず 443 を通す宛先が無い: sbxr は許可先の確認を省いたことを示し、許可外の確認だけを行う
- 8c. 許可外の候補（`example.com`・`example.net`・`example.org`）がすべて許可されている: sbxr は許可外の確認を省いたことを示し、許可先の確認だけを行う
- 8d. VM の中で agent か Docker が使えない: 7a と同じく止まる
- 9a. herdr machine の登録に失敗した: VM は作成済みとして残り、sbxr は登録し直す手順を示して非 0 で終える
- 9b. 同じ `<名前>.sbx` の登録が既にある: sbxr はその登録に触れずに止まり、解除してから登録し直す手順を示す
- 9c. 最初の workspace を VM 内の作業ツリーで開き直せない: VM と herdr machine の登録は残り、sbxr は VM 内で開き直す手順を示して非 0 で終える
- 9d. herdr の pane で agent を起動できない: VM と herdr machine の登録は残り、sbxr は成り立たなかった項目を示して非 0 で終える
- 9e. 宣言に run が無い: sbxr は何も始めない
- 9f. run を始められない: VM は作成済みとして残り、sbxr は非 0 で終える
- 9g. `--attach`: sbxr は最後に、VM 内の作業ツリーで agent の対話 session に入る。利用者が抜けても VM は動いたままにする

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
5. sbxr が、起こし直すコマンド（`sbxr start`）を示す

### Extensions

- 2a. VM が無いか、管理外か、別出所: sbxr は止まる
- 2b. VM 消失: sbxr は destroy を促して止まる
- 2c. 作成途中の VM が止まっているか無い: sbxr は VM に触れずに「止まっている」と示して終える（herdr machine は登録前なので触れない）
- 3a. herdr 連携を有効にせずに作った VM: 4 へ進み、5 を行わない
- 3b. host に herdr が無い: sbxr は VM に触れずに止まる
- 3c. herdr machine の登録が無い（利用者が手で解除した等）: sbxr は警告して 4 へ進み、5 を行わない
- 3d. herdr machine を無効にできない: sbxr は VM を止めずに止まる（止めても herdr が起こし直すため）
- 4a. VM を止められない: sbxr は herdr machine を有効に戻して止まる（VM は動いたままなので、herdr から見失わせない）
- 4b. VM が既に止まっている（外部停止の後など）: sbxr は VM に触れずに「止まっている」と示し、3 で無効にしたなら 5 を行う（herdr が VM を起こし直さないよう、無効化は止まっていても行う。decision/0010）

## UC4 sandbox VM を撤去する

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、VM での作業を終えたか、作り直したい
- 事前条件: sbxr が作った VM（作成途中・VM 消失を含む）の状態ディレクトリがある
- 成功保証: VM と、その sandbox スコープの secret・rule、herdr machine、cache clone、状態ディレクトリが無い

### Main Success Scenario

1. 利用者が VM の撤去を求める
2. sbxr が、sbxr の作った VM で、止まっていることを確かめる
3. sbxr が、VM 内の commit と変更が失われることを示して承認を求める
4. 利用者が承認する
5. sbxr が herdr machine を解除する
6. sbxr が VM を、sandbox スコープの secret・rule ごと撤去する
7. sbxr が cache clone と状態ディレクトリを片付ける

### Extensions

- 2a. VM の状態ディレクトリが無いか、別出所: sbxr は止まる
- 2b. VM が稼働中（作成途中・稼働を含む）: sbxr は、stop してから撤去するか `--force` を付けるよう示して止まる
- 2c. `--force`: sbxr は稼働中でも 3 へ進む
- 2d. herdr 連携を有効にして作った VM で、host に herdr が無い: sbxr は止まる
- 3a. `--yes`: sbxr は確認を省いて 5 へ
- 3b. 端末が無い: sbxr は止まる（`--yes` を案内する）
- 4a. 利用者が承認しない: sbxr は止まる
- 4b. 確認の間に VM が起動した: sbxr は 2b と同じく止まる
- 5a. herdr machine を解除できない: sbxr は解除の手順を警告して 6 へ進み、最後に非 0 で終える
- 6a. VM を消せない: sbxr は状態ディレクトリを残して止まる（撤去には env 定義が要るため）
- 7a. cache clone か状態ディレクトリを消せない: sbxr は警告し、非 0 で終える

却下: 撤去の前に停止中の VM を起こし、origin に push していない commit と変更を検査する。理由: 作業の保全は目的に無く、利用者が VM の中から origin へ push すれば残せる（decision/0023）。

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
- 1b. 宣言が不正（secret 定義以外の key でも）: sbxr は止まる（宣言は create と同じ規則で検証し、重ねる。ADR 0004 の改訂）
- 3a. repo の書式が違う: sbxr は token を尋ねる前に止まる
- 3b. 端末が無いか、値が空: sbxr は止まる
- 4a. 能力が違う（拒否されるべき権限が通る、Contents を読めない）: sbxr は書かずに止まる
- 5a. secret ファイルの mode が 0600 でない: sbxr は止まる

## UC7 宣言の変更を反映する

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、repo 宣言か user 設定を変えた
- 事前条件: sbxr が作った VM（作成途中・VM 消失を含む）の状態ディレクトリがある
- 成功保証: VM が現在の宣言から作り直されている（UC2 の成功保証が成り立つ）

### Main Success Scenario

1. 利用者が、repo 宣言か user 設定を変える
2. 利用者が、作られる内容と drift を確かめる（UC1）
3. 利用者が、VM の作り直しを求める（`sbxr recreate`）
4. sbxr が、作る内容と、VM 内の commit と変更が失われることを一緒に示し、承認を求める
5. 利用者が承認する
6. sbxr が VM を止めて撤去する（UC3・UC4 の、確認より後の段。VM が稼働していなければ、止める段を省く）
7. sbxr が VM を作り直す（UC2 の、確認より後の段）

### Extensions

- 2a. drift が無い: ここで終える
- 2b. 変えたのが user 設定の egress だけ（global rule）: 利用者は UC5 で揃える（既存の VM にも効く）。ここで終える
- 3a. VM が未作成: sbxr は create を促して止まる
- 3b. 同じ名前の VM が管理外か別出所: sbxr は触らずに止まる
- 3c. `--attach` を付けたのに端末が無い: sbxr は VM に触れずに止まる
- 4a. `--yes`: sbxr は確認を省いて 6 へ（git URL なら repo の egress を落とす。UC2 の 3b）
- 4b. 端末が無い: sbxr は止まる（`--yes` を案内する）
- 4c. 現在の宣言が不正か、配線する secret の値が無い: sbxr は VM に触れずに止まる（作り直せないと分かっている VM を消さない）
- 5a. 利用者が承認しない: sbxr は何も変えずに止まる
- 6a. 止められないか撤去できない: sbxr は UC3・UC4 の Extensions のとおりに止まる（作り直しへ進まない）
- 7a. 作り直しの途中で失敗した: sbxr は UC2 の Extensions のとおりに止まる（前の VM は残っていない）
- 7b. cache 層か inputs の内容が変わった: UC2 の 5 で新しい template を作り、旧いものを消す
- 7c. `--attach`: sbxr は最後に、VM 内の作業ツリーで agent の対話 session に入る（UC2 の 9g）

却下: 宣言の変更を sbxr が自動で反映する。理由: 利用者の確認なしに VM 内の変更が失われる（decision/0026）。

## UC8 設定と host の前提を診断する

- Primary Actor: 利用者（AI に設定を書かせるときは、host の agent が利用者の代わりに実行する）
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、user 設定か repo 宣言を書いたか変えた後に、create へ進める状態かを知りたい
- 事前条件: なし（sbx が無くても、sbx に依る項目を skip して他を検査する）
- 成功保証: host の管理状態・sandbox VM・global rule のどれも変わっていない。全項目の結果（ok・fail・skip）が 1 回で示され、fail には直し方が付いている

### Main Success Scenario

1. 利用者が、repo を指定して（省いてもよい）診断を求める
2. sbxr が環境を検査する（sbx が PATH にあり、版が対応する範囲にある、secret ファイルの mode が 0600 で読める、herdr 連携が有効なら host に herdr がある）
3. sbxr が user 設定を検査する（書式・型・未知の key、global rule が宣言と揃っている）
4. sbxr が repo 宣言を検査する（書式・型・未知の key・スコープ制限、repo の egress の group が揃っている）
5. sbxr が merge 後の git identity と、要求された secret が配線されること・その値が secret ファイルにあることを検査する
6. sbxr が全項目の結果を示し、fail が無ければ 0 で終える

### Extensions

- 1a. repo を省いた: sbxr は 4 を行わず、5 を user 設定だけで判定する
- 1b. git URL: sbxr は一時ディレクトリへ clone して repo 宣言を読み、読み終えたら消す（UC1 と同じ）
- 2a. sbx が無い: sbxr は sbx の項目を fail にし、3 の global rule を skip にして続ける
- 2b. secret ファイルが読めない（mode が 0600 でない、書式が違う）: sbxr は fail にし、5 の値の検査を skip にして続ける
- 2c. sbx の版が下限より旧いか、版を読めない: sbxr は fail にし、sbx の更新を示して続ける
- 2d. sbx の版が実測済みの上限より新しい: sbxr は ok のまま、実測済みの範囲の外であることを注記して続ける
- 3a. user 設定が通らない: sbxr は fail にし、user 設定に依る項目（herdr・global rule・git identity・secret の配線と値）を skip にして 4 へ
- 4a. repo 宣言が通らない（clone できない・ディレクトリが無いを含む）: sbxr は fail にし、merge 後の git identity を skip にする。secret は 1a と同じく user 設定だけで判定して 6 へ
- 5a. 要求された secret の注入先 host が egress で許可されていない: sbxr は fail にし、host を許可するか要求から外すよう示す（repo を省いたときは、repo の egress で許可されうるので skip にする）
- 5b. 要求された secret の値が無い: sbxr は fail にし、値を書くコマンド（`sbxr secret setup ...`）を示す
- 6a. fail がある: sbxr は全項目を示してから非 0 で終える

却下: plan に全件検査のモードを足す。理由: plan は最初の誤りで止まる要約で、drift のために VM の状態も読む（decision/0012）。

却下: 環境の検査に host の docker の項目を足す。理由: host に docker が無ければ sbx が builder sandbox で kit を build するので、create は止まらない。止まりうるのは、docker が PATH にあり daemon に繋がらず、kit の build cache も無いときだけで、doctor は cache の有無を判定できない。検査の対象を create が止まる条件に揃えられない（decision/0012。実測は system.md の 2026-11-17 の #116）。その失敗では、sbx の error が daemon の起動を確かめるよう示す。見直し条件: 置かない（create が docker への接続で失敗した件数は、実行の記録から数えられない。見直す引き金は system.md の未実測の前提 13 に置く）

## UC9 AI に設定を書かせる

- Primary Actor: 利用者（host の agent に skill `sbxr-config` で頼む）
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、repo 宣言か user 設定を手で書かずに sbxr を始めたい
- 事前条件: host の agent に skill `sbxr-config` が入っている
- 成功保証: 書いた設定が UC8 の診断を通る。既存のファイルは、利用者が差分を承認したときだけ書き換わっている

### Main Success Scenario

1. 利用者が、host の agent に repo 宣言か user 設定を作るよう頼む
2. agent が、user 設定があるかを確かめる
3. agent が、user 設定が無ければ、利用者に聞いて書く（git identity・使う secret・herdr を使うか・base 固定の profile）
4. agent が、repo の中身（lockfile・Makefile・CI 設定など）から推測して repo 宣言を書く（egress は最小にし、group ごとに理由を書く）
5. agent が、sbxr に診断を求め（UC8）、fail の直し方に従って直す
6. 診断が通り、利用者が UC1 で作られる内容を確かめる

### Extensions

- 1a. user 設定だけを頼まれた: agent は 3 と 5 を行って終える（user 設定があれば、3a に従って直す）
- 3a. / 4a. 既存のファイルがある: agent は既存の内容を読み、差分を利用者に見せて承認を得てから書く。承認されなければ書かない
- 4b. repo が git URL: agent は書かない（生成先はローカル path の repo だけ）
- 5a. 人手の要る fail（secret の値、global rule の収束、sbx・herdr の導入）: agent は利用者に実行するコマンドを示し、利用者が実行してから 5 をやり直す
- 5b. 直しても同じ fail が続く: agent は止まり、残った fail・書き換えたファイル・利用者が次にやることを示す
- 5c. 推測が誤っていたが診断は通った: UC2 の確認プロンプトで利用者が止める

却下: sbxr が agent を呼んで設定を生成する（`sbxr init` など）。理由: 汎用 CLI が agent を subprocess で呼ぶのは標準的でなく、どの agent をどの権限で呼ぶかを CLI が決めることになる（decision/0013）。

## UC10 止めた sandbox VM を起こし直す

- Primary Actor: 利用者
- Scope: sbxr
- Level: user-goal
- Trigger: 利用者が、止めた VM での作業を再開したい
- 事前条件: sbxr が作った、停止中か稼働中の VM がある
- 成功保証: VM が稼働している。VM の中で agent（Claude Code）を起動でき、Docker を使える。herdr 連携の VM では herdr machine が有効で、herdr の pane で agent を起動できる（decision/0025・0027）。作成時の宣言の run が VM の中で始まっている（decision/0028）

### Main Success Scenario

1. 利用者が VM の起こし直しを求める
2. sbxr が、sbxr の作った VM で、作成済みであることを確かめる
3. sbxr が VM を起こす（boot が再生される）
4. sbxr が、VM の中で agent と Docker が使えることを確かめる
5. sbxr が herdr machine を有効に戻し、herdr の pane で agent を起動できることを確かめる
6. sbxr が、作成時の宣言の run を VM 内の作業ツリーで裏で始め、始めたことと、出力が残る VM 内の log の場所を示す

### Extensions

- 1a. `--attach` を付けたのに端末が無い: sbxr は止まる
- 2a. VM が未作成: sbxr は create を促して止まる
- 2b. 作成途中の VM か VM 消失: sbxr は destroy を促して止まる
- 2c. 同じ名前の VM が管理外か別出所: sbxr は触らずに止まる
- 2d. herdr 連携を有効にして作った VM で、host に herdr が無い: sbxr は VM に触れずに止まる
- 3a. VM が既に稼働している（外部起動の後など）: sbxr は VM に触れずに「動いている」と示し、4 へ
- 3b. VM を起こせない: sbxr は止まる（VM は停止中のまま）
- 4a. agent か Docker が使えない: sbxr は成り立たなかった項目を示して非 0 で終える（VM は稼働中のまま）
- 5a. herdr 連携を有効にせずに作った VM: 5 を行わない
- 5b. herdr machine の登録が無い（利用者が手で解除した等）: sbxr は登録し直す手順を示して非 0 で終える
- 5c. herdr machine を有効に戻せないか、pane で agent を起動できない: sbxr は成り立たなかった項目を示して非 0 で終える（VM は稼働中のまま）
- 6a. 作成時の宣言に run が無いか、VM が既に稼働していた（3a）: sbxr は何も始めない（動いている run を二重に始めない）
- 6b. run を始められない: sbxr は非 0 で終える（VM は稼働中のまま）
- 6c. `--attach`: sbxr は最後に、VM 内の作業ツリーで agent の対話 session に入る

却下: create が停止中の VM を起こす。理由: 作成済みの VM への create は drift の報告で、同じコマンドに 2 つの意味が付く（decision/0025）。
