# システム関連図

sbxr の境界と、境界の外の相手との接続を表で書く。語は [CONTEXT.md](../../../CONTEXT.md)、リポジトリ全体の設計判断は [docs/adr/](../../adr/) を正本とする。

## システム境界

境界の内に置くのは次の 3 つ。

- sbxr の CLI（plan / create / stop / destroy / policy sync / secret setup）
- 同梱の資材: default スコープの宣言と、kit `sbxr-boot`・`sbxr-herdr`
- host 側の管理状態: 状態ディレクトリ（sandbox VM と build 用 VM のもの）、cache clone、template の記録（出所ごとに、出所・cache 層の hash・作成日時。状態ディレクトリとは別の置き場に置き、destroy の後も残す）

境界の外に置くのは、利用者、宣言ファイル（user 設定・repo 宣言）、secret ファイル、host 側 repo、sbx とその上の sandbox VM・template・rule・secret、host の herdr、git hosting、GitHub API である。

sandbox VM は sbx の持ち物で、sbxr が触れる手段は sbx の CLI だけである。そのため VM の中へ書くもの（agent runtime profile・git identity・boot）は、Runtime port 越しの出力として扱う。kit は sbxr の成果物だが、走らせるのは VM の dispatcher なので、境界をまたぐ資材として接続表に載せる。

却下: VM の中の構成も境界の内に入れる。理由: sbxr は起動ごとの VM の中を観測できない（2 回目以降の boot の失敗は VM の log に残るだけ）。内に数えると、sbxr が保てない不変条件がドメインモデルに入る。

アクターは利用者だけである。repo 宣言の作者は sbxr に目的を持たないので、アクターではなく untrusted な入力の出所として扱う（確認関門の根拠）。VM 内の agent は sbxr と接しない。

## 相手 × 経由 × 方向

| 相手 | 境界 | 経由 | 方向 | やり取りするもの |
|---|---|---|---|---|
| 利用者 | 外 | CLI 引数・端末（port にしない） | 双方向 | コマンド・投入方式・承認・token / 作る内容の要約・drift・未回収・復旧手順 |
| user 設定 | 外 | ファイル（port にしない） | 入 | 信頼済みの宣言 |
| repo 宣言 | 外 | ファイル（port にしない） | 入 | untrusted な宣言 |
| secret ファイル | 外 | ファイル（port にしない） | 入 / 出（secret setup） | secret の値 |
| host 側 repo（path のとき） | 外 | host の git・ファイル（port にしない） | 入 / mount では VM が直接書く | origin の host、copy で持ち込むもの、mount の対象、cache 層の inputs |
| sbx（sandbox VM・template・rule・secret） | 外 | **Runtime**（port） | 出 + 状態の問い合わせ | env 定義による作成・停止・撤去・状態、VM 内の exec（materialize・init・boot・一時起動・未回収の検査・egress 自己検証・copy の持ち込み）、global rule と sandbox スコープ rule、sandbox スコープの secret、template の save / ls / rm |
| VM 内の kit dispatcher | 外 | 同梱の kit と startup log の文面（port にしない） | 出（kit を置く）/ 入（log を読む） | `sbxr-boot`・`sbxr-herdr`、`fail` 行・`dispatcher complete` 行 |
| host の herdr | 外 | herdr CLI（port にしない） | 出 | herdr machine の登録・無効化・有効化・解除 |
| git hosting | 外 | host の gh / glab / git（port にしない） | 出 | git URL の cache clone、plan と drift の一時 clone |
| GitHub API | 外 | HTTPS（port にしない） | 出 | secret setup github の token の能力の probe |
| origin | 外 | VM 内の git（Runtime の exec 経由） | VM 発 | 未回収の検査のための fetch。VM からの push は agent と利用者が行い、sbxr は関与しない。VM に ssh 鍵は無く token は HTTPS にだけ注入されるので、materialize が origin の host の ssh 形を https に書き換える。fetch と push には、origin の host の egress 許可と secret の配線が要る |
| egress の probe 先 | 外 | VM 内の curl（Runtime の exec 経由） | VM 発 | egress 自己検証の 1 往復ずつ |

### Runtime の順序の制約

sbx v0.45.1 の実測（ADR 0006）から、次の順序を守る。

- sandbox スコープの secret は VM を作る前に置く。作成時に VM の環境変数へ placeholder が入る
- sandbox スコープ rule は VM を作った後にしか置けない
- VM の撤去（env rm）は、VM が無くても sandbox スコープの secret と rule を消す。作成前に失敗して secret だけが残った名前も片付く
- 撤去の時点で env 定義が状態ディレクトリに実在している必要がある。状態ディレクトリは VM を消せた後にしか消さない
- VM の中へのファイルの書き込みは、`sbx exec -i` の stdin を VM 内の shell で書く（`sbx cp` は host の uid と mode のまま置く）
- `sbx stop` は使用中の session も切る。一時起動した VM を止め直すときも同じ

### port の切り方

port は変動性（取り替えの起きやすさ）で決める。

- **Runtime だけを port にする**。実行基盤は差し替えが見込まれ（ADR 0005: microsandbox など）、sbx 自体も experimental な `sbx env` に依存している
- Runtime へは sbx の引数を素通ししない。CLI が要る flag（`--force`・`--yes`・`--workspace` など）を自分で定義し、adapter が実行基盤の引数へ変換する（ADR 0005）
- host の herdr は port にしない。差し替えれば herdr 連携という機能ごと別物になる。interface は test の継ぎ目で、取り替えの境界ではない
- git hosting と GitHub API は port にしない。相手が固定で、継ぎ目は test 用
- kit dispatcher の log の文面は、sbx v0.45.1 の VM の `/etc/durable-startup.d/run.sh` に合わせた約束である。実行基盤を替えれば一緒に変わるので、Runtime の内側に置く
- sbx から sbxr への着信接続は無い。kit は bash で完結し、sbxr を呼ばない。ADR 0006 の「lifecycle から呼ぶ処理は `sbxr` の隠しサブコマンドにする」は実装と食い違っている。ADR の修正は follow-up の issue で扱う

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

イベントは、sbxr が起こす `create`・`stop`・`destroy` と、sbxr の外で起きる `外部起動`（sbx exec・herdr の再接続）・`外部停止`（sbx stop）・`外部撤去`（sbx rm）の 6 つ。同じイベントの結果の分岐（create の失敗の段、destroy の `--force` と未回収）は guard で書く。

主系列: 未作成 →create→ 稼働中 →stop→ 停止中 →外部起動→ 稼働中 →stop→ 停止中 →destroy→ 未作成。

- 管理外と別出所は状態にせず、どのイベントでも触らずに止まる guard として扱う
- herdr machine の登録・無効化・解除と、destroy の一時起動は、遷移の action として書く
- plan・policy sync・secret setup は VM の状態を変えないので、イベントに含めない
- 現行の実装は、VM 消失でも create が「既にある」として drift を報告する。設計では destroy を促して止まる（実装は follow-up）

却下: VM と herdr machine の 2 枚の状態機械。理由: herdr machine の状態は VM の遷移の action として変わるだけで、独立したイベントを持たない（herdr の再接続は VM の外部起動として現れる）。2 枚にすると、同期を note でしか縛れない。

却下: 状態ディレクトリの有無と VM の稼働状態の直積。理由: 管理外と別出所が状態に入り、どのイベントでも「触らない」の uncovered 宣言で埋まる。

## 解こうとしている構造課題

- 回収（#9）: 現行は clone 方式で、sbx が host 側 repo に `sandbox-<name>` remote を足し、README は fetch での取り込みを案内している。取り込みには VM の稼働が要る。git URL の VM では取り込み先の cache clone が destroy で消える。destroy は未回収に気付かない。→ [decision/0001](decision/0001-recover-only-via-origin.md)・[decision/0003](decision/0003-destroy-checks-unrecovered.md)
- 投入方式: 現行は clone に固定で、host の作業ツリーの未 commit の状態を持ち込めず、作業ツリーへの直接の書き込みも選べない。→ [decision/0002](decision/0002-workspace-modes.md)
- init の重さ（#13）: 重い tool の導入が create のたびに init で走る。→ [decision/0004](decision/0004-template-per-repo-built-in-create.md)・[decision/0005](decision/0005-template-build-inputs-and-refresh.md)
- egress（#12）: 宣言した egress が VM で実際に効いているかを、作成時に確かめていない。→ [decision/0006](decision/0006-egress-self-check-from-vm.md)

## 未実測の前提

実装の前に実 sbx で確かめる。確かめられなければ、該当する決定を見直す。

1. sbxenv.yaml で template（image）を指定できるか。`sbx env --help`（v0.45.1）に記述が無い。できなければ、#13 は作成の経路（ADR 0006 の `sbx env create`）から見直す
2. clone 方式（`workspace.clone: true`）の VM 内の repo の origin が、host 側 repo の origin URL を指すか。指さなければ、VM からの push と未回収の検査が成り立たない
3. VM が使っている template の image を `sbx template rm` で消せるか（旧い template の片付け）
4. copy 方式の持ち込み: `sbx cp` は host の uid と mode のまま置く（ADR 0006）。`sbx exec -i` の stdin で流し込み、VM の agent から読み書きできるか
5. mount 方式: env 定義で `workspace.clone: false` にしたとき、host の作業ツリーが VM の agent から読み書きできる uid で見えるか
6. VM 内から許可外の宛先への通信が proxy で拒否され、curl が失敗として返るか（egress 自己検証の判定）
