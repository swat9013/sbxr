# 0014. VM 内の作業ツリーを host と同じ path に置き、herdr の pane をそこから始める

- Status: Accepted
- Date: 2026-09-29
- 覆す条件: [system.md の未実測の前提](../system.md#未実測の前提) 8 が成り立たないとき（copy の置き場を見直す）。9 が成り立たず、登録の後の操作でも herdr の最初の workspace を VM 内の作業ツリーから始められないとき

## 決定

- VM 内の作業ツリーは、投入方式（clone・copy・mount）によらず host 側 repo と同じ path に置く。git URL の VM では cache clone と同じ path になる。[decision/0002](0002-workspace-modes.md) で置き場を決めていなかった copy も、この path に置く
- herdr 連携を有効にした VM では、herdr の pane を VM 内の作業ツリーから始める。herdr 連携の kit が次の 2 つを行う
  - VM の herdr の設定（`~/.config/herdr/config.toml`）の `[terminal] new_cwd` に VM 内の作業ツリーの path を書く（`--cwd` なしで作る pane・tab・workspace に効く）
  - VM 内の herdr server を、VM 内の作業ツリーを cwd にして起動する（server が起動時に作る最初の workspace に効く）
- 始まる場所は宣言で変えられない。VM 内の作業ツリーに固定する
- 手動の `ssh <name>.sbx` で入ったときの cwd は扱わない。`sbx exec` は既に VM 内の作業ツリーから始まるので手を入れない

## 根拠

- mount は sbx の仕様で host と同じ path にしか置けず、clone も sbx が同じ path に置く。残る copy を揃えれば、どの方式でも VM 内の作業ツリーの path が 1 つの規則で決まる
- host と VM で path が同じなので、agent が transcript や log に残す path を host でもそのまま読める
- 実測（2026-09-29、herdr 0.9.1 / VM の herdr v0.9.0）では、herdr の pane は VM 内の herdr server の cwd（sbx の image の既定の cwd `/home/agent/workspace`）で始まり、VM 内の作業ツリーではなかった。`sbx exec` は VM 内の作業ツリーから始まるので、入口によって作業の起点が食い違っていた
- herdr の machine ごとに既定の cwd を持つ設定は無い（`herdr machine add` の option は `--label` と `--remote-session` だけ）。cwd を決めるのは VM 内の server なので、VM 側の設定と server の起動で揃える
- 手動の ssh を揃えるには、VM の shell の設定か host の ssh の設定を変えることになる。herdr と `sbx exec` の外の入口のために、利用者の shell や ssh の挙動を sbxr が変えるのは本意でない

## 却下した代替案

- VM 内の作業ツリーを `~/workspace/<repo>` に置く: clone と mount では sbx の置き方に逆らうことになり、mount では置けない
- VM の `~/.bashrc` で VM 内の作業ツリーへ `cd` する: 手動の ssh にも効くが、herdr の `new_cwd` の `follow` や `--cwd` の指定を上書きしうる。条件付きにしても、shell の挙動を sbxr が変える
- host の ssh の設定（`RemoteCommand`）で `cd` する: sbx が host の `~/.ssh/config` に書く設定を sbxr が書き換えることになる
- 始まる場所を宣言で変えられるようにする（monorepo の一部から始めるなど）: 用途が見えていない。untrusted な repo 宣言が VM の外から見える挙動を左右する範囲が広がる
