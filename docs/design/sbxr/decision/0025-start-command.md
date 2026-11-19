# 0025. 止めた VM を起こし直す `sbxr start` を足す

- Status: Accepted
- Date: 2026-11-19

## 決定

- `sbxr start <repo>` を足す。確認プロンプトは通さない
- 状態ごとの振る舞い
  - 停止中: VM を起こし、herdr 連携を有効にして作った VM なら herdr machine を有効に戻す。稼働中になる
  - 稼働中: VM に触れずに「動いている」と示す。herdr machine が無効なら有効に戻す
  - 作成途中・VM 消失: 拒否して destroy を促す
  - 未作成: 拒否して create を促す
  - 管理外・別出所: 触らずに止まる
- `sbxr stop` が示す案内は、`herdr machine enable <id>` から `sbxr start <repo>` に変える
- sbxr の外での起動（sbx exec・herdr の再接続）は、外部起動として状態機械に残す

## 根拠

- 目的の「1 コマンドで立ち上げ、1 コマンドで止められる」（decision/0023）に対し、止める側だけが 1 コマンドだった。止めた VM を起こすには `sbx exec` を直接打ち、herdr 連携では `herdr machine enable <id>` も要った
- 起こし直しを sbxr の外に置いていたので、起こし直した VM の中で何が成り立つかを、設計のどこも保証していなかった（#87）

## 却下した代替案

- create が停止中の VM を起こす: 作成済みの VM への create は drift の報告で、同じコマンドに 2 つの意味が付く
- start が未作成の VM を作る: create は確認プロンプトを通す操作で、start は通さない操作である。repo 宣言を見せずに VM を作る経路ができる

## 見直し条件

置かない（コマンドの有無の決定で、実行の記録から測れる量が無い）
