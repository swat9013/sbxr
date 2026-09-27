# v0.1.0 の状態ディレクトリ

`app/` は v0.1.0 の sbxr が作った状態ディレクトリ。新しい sbxr がこれを扱えることを `cmd/sbxr/compat_test.go` が固定する（ADR 0006 の改訂）。

- 作った条件: git URL `https://example.com/me/app.git` の repo（repo 宣言は egress `api.example.com:443` を持つ）から、herdr 連携を有効にし、`--yes` で作った。repo の egress を落とした印（`repo-egress-dropped`）を含む
- 作り方: v0.1.0 の source（`git archive v0.1.0`）を展開し、その `cmd/sbxr` に次の test を足して、`FIXTURE_OUT=<出力先> go test ./cmd/sbxr/ -run TestWriteV010Fixture` で書き出した。v0.1.0 の test harness（sbx stub）の上で動く

  ```go
  func TestWriteV010Fixture(t *testing.T) {
  	lc := herdrLifecycle(t)
  	lc.clonedRepoDecl = repoWithEgress
  	lc.mustRun(t, "create", "https://example.com/me/app.git", "--yes")
  	if err := os.CopyFS(os.Getenv("FIXTURE_OUT"), os.DirFS(lc.places.StateDir("app"))); err != nil {
  		t.Fatal(err)
  	}
  }
  ```

- 書き出した後に変えたのは、`sbxenv.yaml` の `workspace.path`（test の一時ディレクトリ）を固定値 `/home/me/.cache/sbxr/repos/app` に置き換えたことだけ

次の版を release したら、同じ手順でその版の fixture を足す。既存の fixture は書き換えない。
