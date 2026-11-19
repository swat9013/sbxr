# システム関連図

sbxr の境界と、境界の外の相手との接続を表で書く。語は [CONTEXT.md](../../../CONTEXT.md)、リポジトリ全体の設計判断は [docs/adr/](../../adr/) を正本とする。

## システム境界

境界の内に置くのは次の 3 つ。

- sbxr の CLI（plan / create / stop / start / destroy / recreate / policy sync / secret setup / doctor）
- 同梱の資材: default スコープの宣言と、kit `sbxr-boot`・`sbxr-herdr`
- host 側の管理状態: 状態ディレクトリ（sandbox VM と build 用 VM のもの）、cache clone。template の記録は持たず、有無と作成日時は sbx の template の一覧から読む（decision/0020）

境界の外に置くのは、利用者、宣言ファイル（user 設定・repo 宣言）、secret ファイル、host 側 repo、sbx とその上の sandbox VM・template・rule・secret、host の herdr、git hosting、GitHub API である。

sandbox VM は sbx の持ち物で、sbxr が触れる手段は sbx の CLI だけである。そのため VM の中へ書くもの（agent runtime profile・git identity・boot）は、Runtime port 越しの出力として扱う。kit は sbxr の成果物だが、走らせるのは VM の dispatcher なので、境界をまたぐ資材として接続表に載せる。

却下: VM の中の構成も境界の内に入れる。理由: sbxr は起動ごとの VM の中を観測できない（2 回目以降の boot の失敗は VM の log に残るだけ）。内に数えると、sbxr が保てない不変条件がドメインモデルに入る。

アクターは利用者だけである。repo 宣言の作者は sbxr に目的を持たないので、アクターではなく untrusted な入力の出所として扱う（確認プロンプトの根拠）。VM 内の agent は sbxr と接しない。host の agent が skill `sbxr-config` で設定を書くとき（UC9）も、agent は利用者の代わりに CLI を叩くだけで、sbxr と接するのは doctor と plan の出力だけである。skill は repo に置くが、sbxr の binary には含めず、境界の外に置く（decision/0013）。

## 相手 × 経由 × 方向

| 相手 | 境界 | 経由 | 方向 | やり取りするもの |
|---|---|---|---|---|
| 利用者 | 外 | CLI 引数・端末（port にしない） | 双方向 | コマンド・投入方式・承認・token / 作る内容の要約・drift・復旧手順 |
| user 設定 | 外 | ファイル（port にしない） | 入 | 信頼済みの宣言 |
| repo 宣言 | 外 | ファイル（port にしない） | 入 | untrusted な宣言 |
| secret ファイル | 外 | ファイル（port にしない） | 入 / 出（secret setup） | secret の値 |
| host 側 repo（path のとき） | 外 | host の git・ファイル（port にしない） | 入 | origin の host、copy で持ち込むもの、cache 層の inputs |
| sbx（sandbox VM・template・rule・secret） | 外 | **Runtime**（port） | 出 + 状態の問い合わせ | env 定義による作成・起動・停止・撤去・状態（absent・stopped・running に読むのは adapter）、VM 内の exec（materialize・init・boot・egress 自己検証・VM の中で agent と Docker が使えることの確認・run の開始・agent の対話 session・copy の持ち込み・herdr の最初の workspace の開き直し）、VM のファイルの読み・書き・有無、herdr が繋ぐ ssh の宛先、global rule と sandbox スコープ rule、sandbox スコープの secret、template の save / ls / rm |
| VM 内の kit dispatcher | 外 | Runtime の内側（Sbx adapter が kit と startup log の文面を持つ） | 出（kit を置く）/ 入（log を読む） | `sbxr-boot`・`sbxr-herdr`、`fail` 行・`dispatcher complete` 行。domain は作る内容（boot の再生、herdr の導入と版、VM 内の作業ツリー、host の skills を持ち込まないこと）を渡し、VM の起動時の処理の完了を adapter が待つ（decision/0009）。このうち host の skills を持ち込まないことは kit ではなく、env 定義の `sandboxOptions.skills` で作成時に入れる（decision/0017） |
| host の herdr | 外 | herdr CLI（port にしない） | 出 | herdr machine の登録・無効化・有効化・解除 |
| git hosting | 外 | host の gh / glab / git（port にしない） | 出 | git URL の cache clone、plan・drift・doctor の一時 clone |
| GitHub API | 外 | HTTPS（port にしない） | 出 | secret setup github の token の能力の probe |
| origin | 外 | VM 内の git（Runtime の exec 経由） | VM 発 | VM からの push と fetch は agent と利用者が行い、sbxr は関与しない。VM に ssh 鍵は無く token は HTTPS にだけ注入されるので、materialize が origin の host の ssh 形を https に書き換える。fetch と push には、origin の host の egress 許可と secret の配線が要る |
| egress の probe 先 | 外 | VM 内の curl（Runtime の exec 経由） | VM 発 | egress 自己検証の 1 往復ずつ |

### Runtime の順序の制約

sbx v0.45.1 の実測（ADR 0006）から、次の順序を守る。順序は Sbx adapter の内側で守る。domain は作る内容を渡し、定義（env 定義と kit）を書く手順と、定義から VM を作る手順を呼ぶだけにする（decision/0009）。

- sandbox スコープの secret は VM を作る前に置く。作成時に VM の環境変数へ placeholder が入る
- sandbox スコープ rule は VM を作った後にしか置けない
- VM の撤去（env rm）は、VM が無くても sandbox スコープの secret と rule を消す。作成前に失敗して secret だけが残った名前も片付く
- 撤去の時点で env 定義が状態ディレクトリに実在している必要がある。状態ディレクトリは VM を消せた後にしか消さない
- VM の中へのファイルの書き込みは、`sbx exec -i` の stdin を VM 内の shell で書く（`sbx cp` は host の uid と mode のまま置く）
- `sbx stop` は使用中の session も切る

### port の切り方

port は変動性（取り替えの起きやすさ）で決める。

- **Runtime だけを port にする**。実行基盤は差し替えが見込まれ（ADR 0005: microsandbox など）、sbx 自体も experimental な `sbx env` に依存している
- Runtime へは sbx の引数を素通ししない。CLI が要る flag（`--force`・`--yes`・`--workspace` など）を自分で定義し、adapter が実行基盤の引数へ変換する（ADR 0005）
- host の herdr は port にしない。差し替えれば herdr 連携という機能ごと別物になる。interface は test の継ぎ目で、取り替えの境界ではない
- git hosting と GitHub API は port にしない。相手が固定で、継ぎ目は test 用
- kit dispatcher の log の文面は、sbx v0.45.1 の VM の `/etc/durable-startup.d/run.sh` に合わせた約束である。実行基盤を替えれば一緒に変わるので、Runtime の内側に置く。env 定義の schema と kit の参照も同じ理由で Runtime の内側に置き、Runtime は作る内容を domain の言葉で受け取る（decision/0009）。sbx が host の skills store を VM の agent の skills ディレクトリへ共有する仕組みと、それを切る env 定義の key（`sandboxOptions.skills`）も sbx 固有の約束なので Runtime の内側に置く。domain は作る内容で「host の skills を持ち込まない」を頼むだけにする（decision/0017）
- template を指定する env 定義には `pullPolicy: never` を書く。template は local の image store にしか無く、その名前を registry へ問い合わせさせない。cache 層の無い宣言では template も `pullPolicy` も書かない。env 定義の key なので Runtime の内側に置く
- sbx から sbxr への着信接続は無い。kit は bash で完結し、sbxr を呼ばない。ADR 0006 の「lifecycle から呼ぶ処理は `sbxr` の隠しサブコマンドにする」は実装していない（ADR 0006 にその旨を追記してある）

## 状態機械

sbxr から見た 1 つの sandbox VM の状態。正本は [statechart.puml](statechart.puml)。

| 状態 | 状態ディレクトリ | 作成時の宣言 | VM |
|---|---|---|---|
| 未作成 | 無し | 無し | 無し |
| 作成途中・停止 | あり | 無し | 停止か、無し |
| 作成途中・稼働 | あり | 無し | 稼働 |
| 稼働中 | あり | あり | 稼働 |
| 停止中 | あり | あり | 停止 |
| VM 消失 | あり | あり | 無し |

イベントは、sbxr が起こす `create`・`stop`・`start`・`destroy` と、sbxr の外で起きる `外部起動`（sbx exec・herdr の再接続）・`外部停止`（sbx stop）・`外部撤去`（sbx rm）の 7 つ。同じイベントの結果の分岐（create の失敗の段、start で VM を起こせたか、destroy の `--force`）は guard で書く。

主系列: 未作成 →create→ 稼働中 →stop→ 停止中 →start→ 稼働中 →stop→ 停止中 →destroy→ 未作成。

- 管理外と別出所は状態にせず、どのイベントでも触らずに止まる guard として扱う
- herdr machine の登録・無効化・有効化・解除と、create・start の終わりに VM の中を確かめることと run を始めることは、遷移の action として書く
- recreate は新しいイベントにせず、stop → destroy → create を続けて起こすものとして扱う（decision/0026）
- plan・policy sync・secret setup・doctor は VM の状態を変えないので、イベントに含めない

却下: VM と herdr machine の 2 枚の状態機械。理由: herdr machine の状態は VM の遷移の action として変わるだけで、独立したイベントを持たない（herdr の再接続は VM の外部起動として現れる）。2 枚にすると、同期を note でしか縛れない。

却下: 状態ディレクトリの有無と VM の稼働状態の直積。理由: 管理外と別出所が状態に入り、どのイベントでも「触らない」の uncovered 宣言で埋まる。

## 解こうとしている構造課題

- VM 内の commit の push（#9）: 現行は clone 方式で、sbx が host 側 repo に `sandbox-<name>` remote を足し、README は fetch での取り込みを案内している。取り込みには VM の稼働が要る。git URL の VM では取り込み先の cache clone が destroy で消える。destroy は、origin に push していない commit と変更に気付かない。→ [decision/0001](decision/0001-recover-only-via-origin.md)。destroy がそれを検査する決定（decision/0003・0021）は取り下げた → [decision/0023](decision/0023-purpose-isolation-development-ease.md)
- 投入方式: 現行は clone に固定で、host の作業ツリーの未 commit の状態を持ち込めず、作業ツリーへの直接の書き込みも選べない。→ [decision/0002](decision/0002-workspace-modes.md)。mount は取り下げた → [decision/0024](decision/0024-workspace-mount-withdrawn.md)
- init の重さ（#13）: 重い tool の導入が create のたびに init で走る。→ [decision/0004](decision/0004-template-per-repo-built-in-create.md)・[decision/0005](decision/0005-template-build-inputs-and-refresh.md)
- egress（#12）: 宣言した egress が VM で実際に効いているかを、作成時に確かめていない。→ [decision/0006](decision/0006-egress-self-check-from-vm.md)・[decision/0007](decision/0007-egress-self-check-verdict-by-proxy-denial.md)・[decision/0016](decision/0016-proxy-denial-is-approval-pending-or-local-rule-block.md)
- VM 内の作業ツリー: copy の置き場が決まっておらず、herdr の pane は VM 内の作業ツリーではなく image の既定の cwd（`/home/agent/workspace`）で始まる。→ [decision/0014](decision/0014-vm-worktree-at-host-path-and-herdr-starts-there.md)・[decision/0015](decision/0015-herdr-first-workspace-reopened-after-registration.md)
- 起こし直し: 止めた VM を起こす手段が sbxr に無く、起こし直した VM の中で何が成り立つかを設計のどこも保証していない（#87）。→ [decision/0025](decision/0025-start-command.md)・[decision/0027](decision/0027-create-and-start-check-inside-vm.md)
- 作り直し: 宣言の変更を反映するのに stop → destroy → create の 3 コマンドが要る。→ [decision/0026](decision/0026-recreate-command.md)
- 立ち上げた後の作業の開始: 準備（init・boot）の後に agent や script を始める仕組みが無く、利用者が VM に入って自分で起動している。→ [decision/0028](decision/0028-run-and-attach-after-create-and-start.md)

## 未実測の前提

実装の前に実 sbx で確かめる。確かめられなければ、該当する決定を見直す。

1. sbxenv.yaml で template（image）を指定できるか。`sbx env --help`（v0.45.1）に記述が無い。できなければ、#13 は作成の経路（ADR 0006 の `sbx env create`）から見直す
2. clone 方式（`workspace.clone: true`）の VM 内の repo の origin が、host 側 repo の origin URL を指すか。指さなければ、VM からの push が成り立たない
3. VM が使っている template の image を `sbx template rm` で消せるか（旧い template の片付け）
4. copy 方式の持ち込み: `sbx cp` は host の uid と mode のまま置く（ADR 0006）。`sbx exec -i` の stdin で流し込み、VM の agent から読み書きできるか
5. mount 方式: env 定義で `workspace.clone: false` にしたとき、host の作業ツリーが VM の agent から読み書きできる uid で見えるか
6. VM 内から許可外の宛先への通信が proxy で拒否され、curl が失敗として返るか（egress 自己検証の判定）。下の実測は sbx v0.45.1 の拒否応答（body の `Blocked by network policy`）を前提にしている。sbx v0.48.0 では許可外の宛先が承認待ち（body の 1 行目が `Approval required for <宛先>.`）になり、この前提は古くなった → [decision/0016](decision/0016-proxy-denial-is-approval-pending-or-local-rule-block.md)
7. sbx が global rule の host の大小文字や port の無い pattern（例: `*.example.com`）を正規化して保存するか。書き換えるなら、`sbxr policy sync` はその宛先を毎回消して足し直す（ADR 0008）
8. copy 方式で、host と同じ path（例: `/Users/<user>/...`）の親ディレクトリを VM 内に agent の所有で作り、そこへ展開できるか（decision/0014）。4 で確かめたのは `/home/agent/workspace/<repo>` だけ
9. `herdr machine add` が ssh 経由で起動し直す VM 内の herdr server でも、最初の workspace が VM 内の作業ツリーで始まるか（decision/0014）。2026-09-29 の実測では、sbxr の kit が起動した server は登録の前に止められ、`machine add` が起動した server の cwd は `/home/agent/workspace` だった。その cwd の決まり方と、`new_cwd` が最初の workspace に効くかは確かめていない。→ 成り立たない（下の実測。#85）
10. Kit spec v3 の kit を使う VM でも、`/var/log/sbx-kit-startup.log` に、Sbx adapter が kit startup の完了と失敗を読む dispatcher の行（上の表の kit dispatcher の行）が今と同じ文面で出るか。herdr を有効にした create はこの行で完了を待つ。文面は v2 runtime の dispatcher（`/etc/durable-startup.d/run.sh`）のもので、v3 の `lifecycle@1` は startup hook を VM の起動のたびに走らせることだけを決め、log の場所も文面も決めていない。v3 への移行の手順（spec の `skills/migrate-kit-to-v3`）は log の path に同じ `/var/log/sbx-kit-startup.log` を案内しているが、文面が同じかは書いていない。成り立たなければ、create は完了を読めず、adapter の待ちの上限の後に失敗する（#74 の項目 2）。→ sbx v0.53.0 では成り立つ（下の 2026-11-16 の実測）
11. env 定義から、状態ディレクトリに書いた v3 の source kit を参照して VM を作れるか。2026-11-16 の実測は `sbx create --kit` で確かめ、`sbx env create` では確かめていない。v3 の kit 名は directory 名で決まる。→ 成り立つ（下の 2026-11-17 の実測）
12. v3 の source kit の build（`docker.io/docker/sandbox-kit:3` の pull）が、sbxr の default の global rule のもとで通るか。→ build は通る。ただし build と pull は VM ではなく host の docker が行い、sbx の proxy を通らない（下の 2026-11-17 の実測）。host に docker が無いときは、sbx の builder sandbox が build する（下の 2026-11-17 の実測の #116）
13. host の docker が PATH にあるが daemon に繋がらず、kit の build cache も無いとき、v3 の埋め込み kit を使う `sbxr create` が kit の build で失敗するか。実装の前提ではないので、確かめずに置く。create が docker への接続で失敗した報告が 1 件出たら確かめ、doctor に注記を足す案（usecases.md の UC8 の却下）を見直す。下の #116 の実測は、descriptor に誤りのある kit と `sbx kit validate` でしか確かめていない
14. `sbx template save` した image について、`sbx template ls --json` の `created_at` が save の時刻になるか（decision/0020）。ならなければ、template の記録を持つ形（decision/0004）へ戻す
15. VM 内の作業ツリーの path を消し、その親ディレクトリだけが残った template から、clone・copy の VM を作れるか（decision/0022）
16. 1〜5 の実測は sbx v0.45.1 のもので、agent が kit になった v0.51.0 より前である。対応する版の下限（decision/0018・0019）で確かめ直すまで、decision/0002〜0005 の実装に入らない
17. VM の中の container からの通信が、VM 直下からの通信と同じ egress の規則と proxy を通るか。通らなければ、VM の中で Docker を使えることが隔離を破る（decision/0023 の優先順では隔離が勝つ）。確かめるまで、egress 自己検証は VM 直下からの 1 往復だけを確かめている。同じ経路を通ると分かれば、egress 自己検証に container からの確認は足さない。別の経路を通るなら、足すか、実行基盤の選び方（ADR 0005）を見直す。→ sbx v0.53.0 では成り立つ。ただし proxy を使わない通信の失敗は、proxy の拒否応答の形をとらない（下の 2026-11-19 の実測）
18. 止めた VM を sbx で起こした後、kit の startup の完了を Sbx adapter が待てるか（create の経路では待てている。decision/0009）。待てなければ、start の終わりに VM の中を確かめる段（decision/0027）が startup より先に走る

5（mount）は、当たる決定を取り下げたので確かめ直さない（decision/0024）。15 の mount の部分も同じく確かめない。2 は、destroy の前の検査（decision/0023 で取り下げた）の前提ではなくなったが、VM からの push の前提として残る。

Kit spec v3 への移行と、対応する sbx の版の下限 → [decision/0018](decision/0018-embedded-kits-move-to-kit-spec-v3.md)。11 と 12 は、移行の実装の前に確かめる。

### 実測（2026-09-27、sbx v0.45.1、#33）

1〜6 を確かめた。1〜5 は成り立ち、6 は後半が成り立たない。7 は #33 の後に足した前提で、確かめていない。env 定義は sbxr が書く形（`schemaVersion: "1"`・`agent: claude`）にそろえ、`sbx env create -y` で probe の VM を 1 つずつ作って確かめた。probe の repo の origin は `https://github.com/example/sbxr-probe.git`（実在しない）。global policy は default deny だった（`sbx policy check network example.com:443` → Denied、`github.com:443` → Allowed）。

1. **成り立つ**
   - template を指定する key は `sandboxOptions.template`。top-level の `template:`・`image:` は `field not found in type sbxenv.Config` で拒否される
   - 手順: clone 方式の VM に marker（`/home/agent/.sbxr-probe-template-marker`）を置き、`sbx template save <VM> sbxr-probe:t1` で save した。別の VM を `sandboxOptions.template: docker.io/library/sbxr-probe:t1`・`pullPolicy: never` で作ると、create の出力の image がその tag になり、marker があった
   - save した tag は `docker.io/library/<name>:<tag>` に正規化される
   - `template save` は稼働中の VM を拒む（`cannot save a running sandbox`）。止めてから save する
   - `sbx env plan` は存在しない image を指定しても通る。image の有無は create まで分からない
   - 確かめたのは `pullPolicy: never` のときだけ。既定の `always` で local にしか無い tag を指定したときは確かめていない
2. **成り立つ**
   - 手順: probe の repo から clone 方式（`workspace.clone: true`）の VM を作り、`sbx exec -w <repo> <VM> git remote -v`・`git branch -vv`・`git for-each-ref` で VM 内を見た。host 側 repo は、create の後と `sbx env rm` の後に `git for-each-ref` と `git remote -v` で見た
   - VM 内の repo の `git remote -v` は origin だけで、host 側 repo の origin URL を指す。`main` は `origin/main` を追跡する
   - fetch の前の VM の `refs/remotes/origin/*` は、実際の origin ではなく、create 時の host の local branch を写したもの（origin に無い host の commit を指していた）。未 push の作業の検査を「VM 内で fetch した後」の remote-tracking で行う decision/0003 の定義は、この理由でも外せない
   - host 側 repo には `sandbox-<name>` remote（`git://127.0.0.1:<port>/<repo>`）と `refs/sandboxes/<name>/*` が足される。`sbx env rm` の後、remote は消え、`refs/sandboxes/<name>/*` は残る
3. **成り立つ**
   - 手順: 1 の VM が稼働中のまま、その image の tag を `sbx template rm --force sbxr-probe:t1` で消した。`Removed` で 0 で終わり、`sbx template ls` から消えた
   - `sbx template rm` は stdin が端末でないと `--force` を要求する。`--force` は確認を省くだけ
   - 消した後も VM は動き続け、`sbx stop` の後の `sbx exec` による再起動も通った
4. **成り立つ**
   - 手順: workspace を持たない VM（env 定義に `workspace:` を書かない）で、host で `tar -cf -` した `.git`・tracked・ignored でない untracked のファイルを `sbx exec -i <VM> tar -xf - -C <dir>` に流した。展開したファイルは agent（uid 1000）の所有になり、agent で追加のファイルを作って commit できた
   - 確かめた置き場は `/home/agent/workspace/<repo>`（exec の既定の cwd で、agent の所有）。host と同じ path に置くときの親ディレクトリの作成は確かめていない
   - 対照として、`sbx cp` で置いたファイルは host の uid（1917701712）と gid 0 のままだった（ADR 0006 の記述どおり）
5. **成り立つ**
   - 手順: `workspace.clone: false` の VM で、VM 内の host と同じ path の作業ツリーを見た。ファイルは uid 1000（agent）の所有に見え、agent でファイルを作り、commit できた。VM で作ったファイルは host では host の uid で、commit は host 側 repo の log に現れた
   - ignored のファイル（`.gitignore` に書いた `ignored.txt`）も VM から読めた。decision/0002 が確認プロンプトで示すとしたリスクのとおり
6. **前半は成り立ち、後半は成り立たない** → #39
   - 手順: 2 の VM で `sbx exec <VM> curl -sS -o /dev/null -w '%{http_code}' https://example.com` と、同じ形の `https://github.com` を実行した。あわせて `-f`・`--noproxy '*'`・`http://` の変形と、body の中身を見た
   - 許可外の `https://example.com` は proxy（`gateway.docker.internal:3128`）が拒否する。ただし応答は HTTP 403 で、既定の `curl` は exit 0 で返る
   - 403 の body は `Blocked by network policy: domain example.com:443`。TLS は proxy が終端しているので、状態コードだけでは宛先自身の 403 と区別できない
   - `curl -f` は許可外で exit 22、許可先の `https://github.com` で exit 0 を返す。ただし、宛先が 4xx を返すと誤判定する
   - `--noproxy '*'` では、許可外は exit 6（名前解決の失敗）、許可先は 200 だった。`http://example.com` も 403 で拒否された
   - decision/0006 の核（VM 内から実際の通信で 1 往復ずつ確かめる）は成り立つ。「届く」「届かない」の判定は、#39 で proxy の拒否応答（403 と body の `Blocked by network policy`）に置き換えた → [decision/0007](decision/0007-egress-self-check-verdict-by-proxy-denial.md)。この body は sbx v0.48.0 では返らず、拒否応答の定義は v0.48.0 の承認待ちと deny rule の拒否に置き換えた → [decision/0016](decision/0016-proxy-denial-is-approval-pending-or-local-rule-block.md)

### 実測（2026-09-29、sbx v0.46.0、host の herdr 0.9.1 / VM の herdr v0.9.0、#85）

9 を確かめた。成り立たない。clone 方式の probe の VM を `sbxr create` で作り、VM 内で `herdr` の CLI と `/proc/<pid>/cwd`・`/proc/<pid>/environ` を見た。

9. **成り立たない** → [decision/0015](decision/0015-herdr-first-workspace-reopened-after-registration.md)
   - 手順: VM に `~/.config/herdr/config.toml`（`[terminal] new_cwd = "<作業ツリー>"`）を置き、create と同じく server を止めて session を消してから、host で `herdr machine add` し直した
   - `machine add` が起動した server の cwd は `/home/agent/workspace` で、server の log は `created startup workspace cwd=/home/agent/workspace` だった。最初の workspace は server の cwd で始まり、`new_cwd` は効かない
   - `new_cwd` は、`--cwd` なしの `herdr tab create`・`herdr workspace create`・`herdr pane split` に効いた（応答の `cwd` が作業ツリー）
   - 登録の後に `herdr workspace create --cwd <作業ツリー> --focus` → `herdr workspace close w1` とすると、残る workspace は作業ツリーの 1 件になる。その pane は `/bin/bash` で、`claude` が PATH で見つかり、sbx の proxy を持つ
   - 保存された session があると、server は起動時の workspace を作らずに session を復元する（`restored session already has workspaces; ignoring startup cwd`）。`herdr server` の正常な停止は session を消す（`session cleared`）ので、create の経路では復元は起きない
   - 作業ツリーは、create の初回 boot の kit の startup の時点で既にある
   - kit が起動した server（`/usr/local/bin/herdr`、`SHELL=/bin/sh`、PATH に `~/.local/bin` が無い）には、動いたままだと `machine add` できない（ADR 0007 の実測を再現した）。`~/.local/bin/herdr server` で起動した server なら、動いたままでも登録できた
   - 起動し直した VM では、kit の server が VM 内の作業ツリーを cwd にして起動し、保存された pane も作業ツリーのまま復元された。ただし復元された pane の shell は `/bin/sh` だった（kit の環境を継ぐ）

### 実測（2026-11-16、sbx v0.53.0、host の herdr 0.9.5）

10 を確かめた。成り立つ。あわせて、sbx v0.51.0 の破壊的変更が `sbxr create` に及ぶことを確かめた。probe の repo は 2026-09-27 の実測と同じ形（origin は実在しない `https://github.com/example/sbxr-probe.git`）で、user 設定は herdr 連携を有効にしている。

- **`sbxr create` は VM を作る段で失敗する**
  - 手順: main（7c1483e）の sbxr で `sbxr doctor` が ok 9 / fail 0 で通ることを確かめてから、`sbxr create --yes <probe の repo>` を実行した
  - `sbx env create --force` が次の出力で exit 1 を返した。`○ claude — now ships as a kit; resolving docker.io/sbx/claude:latest`、`error: resolve kits: cannot mix v3 kits with v1/v2 kits in one sandbox; compositions are single-format (v3: claude; v1/v2: sbxr-herdr, sbxr-boot).`
  - 拒否は kit の解決の段で起き、VM は作られない。状態は作成途中・停止になり、`sbxr destroy --yes` で未作成に戻せた
  - `sbxr-boot` は herdr 連携の有無によらず入るので、herdr 連携を無効にしても同じ拒否になる見込み（無効の create は実行していない）
  - sbx v0.51.0 の release notes（Breaking changes）: built-in agent の名前は `docker.io/sbx/<name>:latest` の公開 kit に解決される。「V1 and v2 kits cannot be combined with v3 kits. Upgrade older mixins to v3 before combining them with the new built-in kits.」
10. **成り立つ**（sbx v0.53.0、kit の frontend は `sandbox-kit 3.0.0-m.11`）
   - 手順: `lifecycle@1` の startup に、成功する段（`echo`）と失敗する段（`exit 3`）を 1 つずつ持つ v3 の mixin kit（`schemaVersion: "3"`・`kind: mixin`）を作り、`sbx create --clone --skills off --kit <kit> claude <probe の repo>` で VM を作って、VM 内の `/var/log/sbx-kit-startup.log` と `/etc/durable-startup.d/run.sh` を読んだ
   - log の場所は同じ `/var/log/sbx-kit-startup.log` で、実行ごとに `=== dispatcher run <時刻> ===` で始まる
   - 失敗した段は `fail /etc/durable-startup.d/002-startup-probe-v3/001-cmd.sh exit=3` と出て、dispatcher はそこで止まる。Sbx adapter が失敗を読む形（`fail /etc/durable-startup.d/` で始まり ` exit=` を含む行）に当たる
   - 完了の行 `=== dispatcher complete ===` は、`run.sh` が全段の成功の後に出す（`run.sh` の source で確かめた。probe は失敗する段を持つので、log では見ていない）
   - script の path は `/etc/durable-startup.d/<連番>-startup-<kit 名>/<連番>-cmd.sh`。built-in の claude の kit も同じ dispatcher で startup を走らせ、その行が同じ log に先に出る
   - `sbx stop` の後に `sbx exec` で起動し直すと、`=== dispatcher run` が 2 回になった（startup は起動ごとに走る）
   - startup の段が失敗しても、`sbx create` は警告（`warning: a kit startup command failed; view the log: ...`）を出して exit 0 で終わる
   - v3 の descriptor は top-level の `name:` を受けない（`field name not found in type spec.Descriptor`）。kit 名は directory 名から決まる（log の `probe-v3`）
   - v3 の source kit の検証と作成は kit の build を要し、`sbx kit validate --offline` は通らない（`this load path has no kit builder configured`）。build は `docker.io/docker/sandbox-kit:3` を pull する

### 実測（2026-11-17、sbx v0.53.0、#111）

11 と 12 を確かめた。11 は成り立つ。12 は build が通るが、前提が置いた条件（sbx の global rule のもとでの pull）とは経路が違った。probe の repo は 2026-09-27 の実測と同じ形（origin は実在しない `https://github.com/example/sbxr-probe.git`）。

11. **成り立つ**
   - 手順: sbxr が書く形の env 定義（`schemaVersion: "1"`・`agent: claude`・`workspace.clone: true`・`sandboxOptions.skills: off`・`kits: - source: ./kits/probe-v3`）と、同じディレクトリの `kits/probe-v3/spec.yaml` に v3 の mixin kit を置き、`sbx env create --force <dir>` で VM を作った（Sbx adapter と同じ経路）
   - kit の startup が走り、`/var/log/sbx-kit-startup.log` に `ok /etc/durable-startup.d/002-startup-probe-v3/000-cmd.sh` と `=== dispatcher complete ===` が出た。kit 名は directory 名（`probe-v3`）
   - 文字列で書いた startup の command は `sh -c` で包まれ、uid 1000 で走った
   - descriptor の書き方（公開の kit `docker.io/sbx/claude:latest` の `/usr/share/sandbox/kit/claude/kit.yaml` に合わせた）:
     - 1 行目に `# syntax=docker.io/docker/sandbox-kit:3` が要る。無いと build が `spec.yaml` を Dockerfile として読み、`unknown instruction: schemaVersion:` で失敗する
     - startup は `capabilities:` の `- type: com.docker.sandbox/lifecycle@1` の `config.startup` に書く。top-level の `lifecycle@1:` は `field lifecycle@1 not found in type spec.Descriptor` で拒否される
12. **build は通る。経路は前提と違う**
   - build は host の docker（この host では colima）が行い、`docker.io/docker/sandbox-kit:3`（`sandbox-kit 3.0.0-m.11`）を host から pull した。VM の proxy を通らないので、sbx の global rule は build に効かないと読める（global rule を sbxr の default だけにした状態では確かめていない。この host の global rule は `docker.io:443` を許可していた）
   - v3 の source kit を使う create には、host に docker が要る。doctor はこれを検査していない → 前半は成り立たない（下の #116 の実測）。doctor に検査は足さない（usecases.md の UC8 の却下）

### 実測（2026-11-17、sbx v0.53.0、#116）

12 の「v3 の source kit を使う create には、host に docker が要る」を確かめた。成り立たない。`sbx kit validate <dir>` に v3 の mixin kit（`kind: mixin`、`lifecycle@1` の startup だけ）を渡し、host の docker の条件を変えた。`sbx env create` では確かめていない。

- docker が PATH に無い: sbx が builder sandbox（`sbx-kit-builder`）を作って build する。出力は `building in builder sandbox sbx-kit-builder…` と `registry facade path failed (frontend image docker/sandbox-kit:3 is not in the engine store and no host docker can seed it); falling back to archive delivery`。`sbx kit builder --help` は、source-form の kit の build は、最初の build で作られる共有の builder sandbox で走ると書いている
- docker が PATH にあり daemon に繋がらない（`DOCKER_HOST` を実在しない socket にした。`DOCKER_CONTEXT` を daemon の止まった context にした）: build は `failed to connect to the docker API at <socket>; check if the path is correct and if the daemon is running` で失敗し、builder sandbox へは切り替わらない。この 2 回は descriptor に誤りのある kit（top-level の `name:`）で試し、接続の失敗は descriptor を読む前に出た
- build が 1 度通った後は、上のどちらの条件でも、startup の command を変えた kit と増やした kit が、build を走らせずに `valid (directory)` になる。cache の鍵と置き場は特定していない。`sbx kit inspect` は Template を `sbx-kit-src:spec-<hash>` と示す
- v3 の descriptor には `kind:` が要る。無いと `kind must be "workload", "mixin", or "set", got ""` で失敗する

### 実測（2026-11-19、sbx v0.53.0、VM の Docker 29.10.0、#135）

17 を確かめた。成り立つ。VM の中の container・`--network host` の container・`docker build` の `RUN` は、VM 直下と同じ global rule で判定され、許可外の宛先には 1 つも届かなかった。ただし、拒否の形は経路で変わる。proxy を明示した通信は VM 直下と同じ拒否応答（HTTP 403 と `Approval required for <宛先>.`）を受ける。proxy を使わない通信は、名前解決の失敗か接続の切断で終わる。

- VM の作り方: sbxr が書く形の env 定義（`schemaVersion: "1"`・`agent: claude`・`workspace.clone: true`・`sandboxOptions.skills: off`。kit なし）を `sbx env create --force <dir>` に渡した（Sbx adapter と同じ経路）。`sbxr create` は使っていない。この host の user 設定が herdr 連携を有効にしており、`sbxr create` は host の herdr へ machine を登録するため。通信の経路を決めるのは VM と sbx の proxy で、kit は関わらないと読んでいる（herdr と boot の kit を入れた VM では確かめていない）
- global rule: default deny で、`github.com:443`・`**.docker.io:443` などを許可していた（`sbx policy check network example.com:443` → Denied）。global rule は変えていない
- image の pull: VM の Docker daemon による `docker pull curlimages/curl:8.11.1` は、blob の取得先 `production.cloudfront.docker.com:443` が許可されておらず、`403 Forbidden` で失敗した（`sbx policy log` の PROXY は `forward`）。daemon の pull も同じ rule で判定される。この宛先を sandbox スコープ rule で許可して pull した
- 手順: 同じ shell script を、VM 直下（`sbx exec`）、既定の bridge network の container（`docker run`）、`docker run --network host`、`docker build` の `RUN`（既定と `--network host`）で走らせた。script は curl で、proxy を使わない通信（`--noproxy '*'`）と、proxy（`-x http://gateway.docker.internal:3128`）を明示した通信を 1 往復ずつ試す
- container には proxy の環境変数（`HTTPS_PROXY` など）も proxy の CA も入らない。VM 直下には入る。container の既定の通信は、proxy を使わない形になる

結果（括弧で断った行のほかは、どの経路でも同じだった）:

| 通信 | 結果 |
|---|---|
| proxy なし、`https://github.com`（許可先） | 200 |
| proxy なし、`https://example.com` / `http://example.com` | curl exit 6（`Could not resolve host: example.com`） |
| proxy なし、example.com の IP へ `--resolve` で `https://` | curl exit 35（TLS の接続が切れる） |
| proxy なし、example.com の IP へ `--resolve` で `http://` | curl exit 52（`Empty reply from server`。container で確かめた） |
| proxy なし、`https://1.1.1.1` | curl exit 35 |
| proxy なし、`8.8.8.8` への DNS（UDP 53。container で確かめた） | `Connection refused` で応答なし |
| proxy あり、`https://github.com` | 200（VM 直下と、proxy の CA を渡さない container で確かめた。`docker build` では CA だけを信頼させて試し、curl exit 60 だった）。許可先の TLS は proxy が終端せず、`sbx policy log` の PROXY は `forward-bypass` |
| proxy あり、`https://example.com` | 403、body の 1 行目は `Approval required for example.com:443.`（container と `docker build` には proxy の CA を渡した。渡さないと curl exit 60（`self-signed certificate in certificate chain`）） |
| proxy あり、`http://example.com` | 403、body の 1 行目は `Approval required for example.com:80.` |

- `sbx policy log <VM>` は、proxy を使わない通信を PROXY `transparent`、proxy を明示した通信を `forward` で記録し、どちらの拒否も `No matching allow rule (default deny)` だった。名前解決は `DNS lookup blocked by proxy policy`、UDP は `Direct UDP connections not allowed` で拒否された。IP を指定した `https://` も、TLS の SNI の `example.com:443` として判定された
- 拒否された宛先は承認待ち（`sbx policy approval ls`）に載った。`sbx env rm` の後は `No pending approvals.` になった
- VM 直下でも、proxy を使わない通信は container と同じく exit 6 と exit 35 で終わる。拒否の形を分けるのは、container か VM 直下かではなく、proxy を使うかどうか
