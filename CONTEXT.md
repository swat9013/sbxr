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

### egress

**egress 宣言**:
sandbox VM から外部へ出てよい宛先の宣言。sbxr 独自の形式で書き、実行基盤の rule へ変換される。
_Avoid_: network.json, policy file

**global rule**:
user 設定の egress 宣言から作られ、全 sandbox VM に常時適用される許可。
_Avoid_: default allow

**sandbox スコープ rule**:
repo 宣言の egress 宣言から作られ、その sandbox VM にだけ適用される許可。destroy で消える。

### secret

**secret ファイル**:
`~/.config/sbxr/secrets.env`。secret の値の唯一の取得元。
_Avoid_: keychain, `.sbox/env`

**secret 定義**:
`secret_defs.<name>`。注入方式、secret ファイルのキー、注入先 host、VM に見せる環境変数名を束ねる。default と user スコープだけが持てる。

**secret 要求**:
`secrets` に secret 定義の名前を並べて配線を求めること。全スコープで書け、user スコープの要求は全 sandbox VM への常時要求になる。

**placeholder 注入**:
VM には置換用の仮の値だけを入れ、実値は host 側の proxy が通信時に差し込む方式。実値は VM に入らない。
_Avoid_: env 注入

### VM 内の構成

**agent runtime profile**:
sandbox VM 内の agent（Claude Code）の設定。宣言の `profile` から作られ、host 側の個人設定は持ち込まない。
_Avoid_: dot_claude, host settings

**materialize**:
agent runtime profile と git identity を VM 内へ書き出す段階。

**init**:
create 時に 1 回だけ VM 内で走るコマンド列。

**boot**:
sandbox VM の起動ごとに VM 内で走るコマンド列。起動で消える状態（daemon など）を戻す。走るのは作成時に確定した内容。
_Avoid_: startup（sbx kit の用語）, resume hook

**回収**:
sandbox VM 内で agent が作った commit を host 側へ取り込むこと。
_Avoid_: sync, pull
