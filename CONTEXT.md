# sbxr

repo を宣言 1 枚で AI coding agent 用の隔離環境（sandbox VM）にする CLI の語彙。

## Language

### 実体

**sbxr**:
repo（path | git URL）を sandbox VM にする CLI。sandbox 構成の唯一の入口で、3 スコープの宣言を merge して VM へ適用する。
_Avoid_: sbx-repo, sbx-repo.sh（旧実装の名前）

**sandbox VM**:
Docker Sandboxes（`sbx`）の microVM。agent が動く隔離環境で、sbxr の宣言だけで構成される。起動（create / 停止後の再起動）ごとに boot が走る。
_Avoid_: sandbox（単独では sbx の概念全般と紛れる）, container

**状態ディレクトリ**:
sandbox VM ごとに host 側へ置く、作成時に確定した宣言と資材の置き場。destroy と drift 検出はこれを基準にする。
_Avoid_: cache, work dir

**出所**:
sandbox VM を作った repo の絶対 path か git URL。状態ディレクトリに記録し、同じ名前の別 repo の VM を取り違えないために使う。

**cache clone**:
git URL から作る sandbox VM のために、host 側へ clone した repo。`${XDG_CACHE_HOME:-~/.cache}/sbxr/repos/<name>/` に置き、destroy で消える。

**template**:
cache 層を焼いた sandbox VM の snapshot（sbx の template）。出所ごとに作り、cache 層の内容の hash で識別する。destroy では消さず、期限を過ぎるか新しい hash のものを作ると作り直す・消す。
_Avoid_: base image, cache image

**build 用 VM**:
template を作るためだけに一時的に立てる sandbox VM。cache 層の inputs のファイルだけを置き、template の作成が終われば撤去する。

### 宣言

**スコープ**:
宣言の層。default（CLI に同梱）→ user → repo の順に重なり、後の層ほど強いが、repo は制限付きでしか上書きできない。
_Avoid_: layer, level

**repo 宣言**:
repo root の `sbxr.yaml`。repo スコープの宣言で、repo 由来の untrusted な入力として扱う。
_Avoid_: `.sandbox.yaml`（旧名）, repo config

**user 設定**:
`~/.config/sbxr/config.yaml`。user スコープの宣言で、信頼済みの入力として扱う。
_Avoid_: global config, defaults

**確認関門**:
create の前に merge 結果を人間に見せて承認を得る段階。repo 宣言が untrusted であることへの防御。
_Avoid_: prompt, confirm

**drift**:
状態ディレクトリにある作成時の宣言と、現在の宣言との差のうち、既存の sandbox VM が取り込まない部分。drift は再作成の合図になる。global rule は policy sync で既存の sandbox VM にも行き渡るため、drift に含めない。
_Avoid_: 宣言全体の差

**作成時の宣言**:
create のときに確定し、状態ディレクトリに記録した宣言。記録があることが作成済みの印で、drift の基準になる。

**現在の宣言**:
いまの 3 スコープの宣言から、作成時と同じ扱い（投入方式・repo の egress を落としたか）で確定した宣言。

**投入方式**:
repo を sandbox VM へ渡す方式。clone（VM 内の clone）・copy（`.git` と ignored でないファイルの複製。`.git/config` の認証設定は除く）・mount（host の作業ツリーの直接 mount）の 3 つ。create のときに利用者が選び、git URL は clone だけ。
_Avoid_: workspace mode（sbx の用語と紛れる）

**cache 層**:
repo の source を渡す前に走らせ、template に焼くコマンド列。宣言の `template`（`run`・`inputs`）で書く。
_Avoid_: init の前半

### egress

**egress 宣言**:
sandbox VM から外部へ出てよい宛先の宣言。sbxr 独自の形式で書き、実行基盤の rule へ変換される。
_Avoid_: network.json, policy file

**global rule**:
default と user 設定の egress 宣言から作られ、全 sandbox VM に常時適用される許可。
_Avoid_: default allow

**sandbox スコープ rule**:
repo 宣言の egress 宣言から作られ、その sandbox VM にだけ適用される許可。destroy で消える。

**egress 自己検証**:
create の最後に、VM 内から許可先に届くことと許可外に届かないことを 1 往復ずつ確かめる段。通らなければ作成時の宣言を書かない。

### secret

**secret ファイル**:
`~/.config/sbxr/secrets.env`。secret の値の唯一の取得元。
_Avoid_: keychain, `.sbox/env`

**secret 定義**:
`secret_defs.<name>`。注入方式、secret ファイルのキー、注入先 host、VM に見せる環境変数名を束ねる。default と user スコープだけが持てる。

**secret 要求**:
`secrets` に secret 定義の名前を並べて配線を求めること。全スコープで書け、user スコープの要求は全 sandbox VM への常時要求になる。

**配線**:
secret 要求のうち、注入先 host がすべて egress で許可されたものを、sandbox スコープの secret として実行基盤に置くこと。

**placeholder 注入**:
VM には置換用の仮の値だけを入れ、実値は host 側の proxy が通信時に差し込む方式。実値は VM に入らない。
_Avoid_: env 注入

### VM 内の構成

**agent runtime profile**:
sandbox VM 内の agent（Claude Code）の設定。宣言の `profile` と、有効にした herdr 連携から作られ、host 側の個人設定は持ち込まない。ただし投入方式が mount のときは、repo 内の ignored な個人設定（`.claude/settings.local.json` など）も VM から見える。
_Avoid_: dot_claude, host settings

**materialize**:
agent runtime profile と git identity を VM 内へ書き出す段階。

**init**:
create 時に 1 回だけ VM 内で走るコマンド列。

**boot**:
sandbox VM の起動ごとに VM 内で走るコマンド列。起動で消える状態（daemon など）を戻す。走るのは作成時に確定した内容。
_Avoid_: startup（sbx kit の用語）, resume hook

**回収**:
sandbox VM 内で agent が作った commit を、VM が消えても残る場所（origin）へ出すこと。VM 内から push して行う。sbxr は host 側 repo へ取り込む経路を持たない。
_Avoid_: sync, pull, fetch

**未回収**:
destroy で失われる VM 内の作業。origin から到達できない local branch の commit、未 commit の変更、stash。投入方式が clone か copy の VM だけが持つ。

**一時起動**:
destroy が未回収を検査するために、停止中の sandbox VM を起動し、検査の後に止め直すこと。

### herdr 連携

**herdr 連携**:
sandbox VM に herdr を入れ、host の herdr から VM 内の agent を扱えるようにする opt-in の支援機能。user 設定でだけ有効にできる。

**herdr machine**:
herdr 連携を有効にした sandbox VM が、host の herdr に登録される接続先。create で登録し、stop で無効化し、destroy で解除する。
_Avoid_: remote, host entry

### sandbox VM の状態

sbxr から見た 1 つの sandbox VM の状態とイベント。遷移の正本は `docs/design/sbxr/statechart.puml`。

**未作成**:
状態ディレクトリも VM も無い。

**作成途中**:
状態ディレクトリはあるが作成時の宣言が無い（create が途中で止まった）。VM が稼働していれば作成途中・稼働、止まっているか無ければ作成途中・停止。create はやり直さず、destroy を促す。

**稼働中** / **停止中**:
作成時の宣言があり、VM が稼働している / 止まっている。

**VM 消失**:
作成時の宣言はあるが、VM が sbxr の外で撤去されている。destroy で片付ける。

**管理外**:
状態ディレクトリの無い VM。sbxr はどのコマンドでも触らない。

**別出所**:
同じ名前の状態ディレクトリが別の出所のもの。sbxr はどのコマンドでも触らない。

**外部起動** / **外部停止** / **外部撤去**:
sbxr の外（sbx exec・herdr の再接続 / sbx stop / sbx rm）で VM が起動・停止・撤去されること。
