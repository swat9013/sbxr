# 0013. 設定の作成は repo に置く skill として配り、CLI は生成しない

- Status: Accepted
- Date: 2026-09-29
- 覆す条件: CLI 系ツールが agent を subprocess で呼んで設定を作るのが標準的な配り方になったとき。または doctor の error だけでは skill が設定を直しきれない版ずれが続いたとき（schema を出す仕組みが要る）

## 決定

- repo 宣言（`sbxr.yaml`）と user 設定（`~/.config/sbxr/config.yaml`）を AI に書かせる手順は、Agent Skills の skill 1 つ（`skills/sbxr-config/SKILL.md`）として repo に置く。利用者は `gh skill install swat9013/sbxr sbxr-config --agent claude-code --scope user` などで入れる
- sbxr 自身は設定を生成しない（agent を呼ばない）。host の agent が skill の手順に従って書き、`sbxr doctor [<repo>]` で検査して直す。sbxr と skill が接するのは doctor の出力だけ
- skill の中で、repo 宣言と user 設定を別々に作る
  - repo 宣言: repo の中身（lockfile・Makefile・CI 設定など）から推測して書く。egress は最小にし、group ごとに rationale を書く。生成先はローカル path の repo だけ。推測の誤りは create の確認関門で止まる
  - user 設定: 人に聞いて埋める。egress は書かせず、default スコープの同梱 group に任せる（user の egress は全 sandbox VM の許可になる）
  - user 設定が無いまま repo 宣言を頼まれたら、先に user 設定を作る
- 既存のファイルは読み、差分を人に見せて承認を得てから書く
- skill には schema の全体を書かず、最小の書き方の規則と、repo 宣言に書ける key だけを置く。版ずれは doctor の error（どの key が・なぜ駄目で・どう直すか）が埋める

## 根拠

- CLI 系ツールの skill は、`skills/<name>/SKILL.md` を GitHub repo に置き、`gh skill install` / `npx skills add` で入れるのが標準の配り方である（cli/cli 自身も `skills/gh-skill/SKILL.md` を置く）。利用者は使い慣れた agent と、その agent の権限設定のまま使える
- 汎用 CLI が agent を subprocess で呼ぶのは標準的でない。どの agent を・どの権限で・どの課金で呼ぶかを CLI が決めることになる
- 設定の誤りは doctor が全件、直し方付きで返す（decision/0012）。skill はその出力を読んで直せばよく、schema を skill に写して保つ必要が無い

## 却下した代替案

- CLI が対話の prompt を出して設定を作る（`sbxr init` のような wizard）: repo の中身から推測する仕事は agent の方が得意で、CLI に推測の規則を持たせると、規則が言語やツールの数だけ増える
- CLI が `claude -p` などの agent を呼ぶ: 上の「汎用 CLI が agent を呼ぶ」問題。特定の agent に縛られる
- binary に skill を同梱し、コマンドで書き出す: 配り方が標準から外れ、`gh skill update` の追従も使えない。binary の版と skill の版を別々に上げられない
- schema を出すコマンドを足し、skill が読む: schema は key の形しか伝えず、スコープ制限や merge 後の検証（egress group が揃っているか）は伝わらない。doctor の error で足りる
