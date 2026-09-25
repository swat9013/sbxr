package main

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/secret"
)

// prompter は利用者から入力を受け取る。Hidden は入力を画面に出さない。Confirm は y/N を尋ね、端末が無ければ error を返す。
type prompter interface {
	Line(prompt string) (string, error)
	Hidden(prompt string) (string, error)
	Confirm(prompt string) (bool, error)
}

// githubSecretName は default スコープに同梱する GitHub の secret 定義の名前。
const githubSecretName = "github"

func newSecretCmd(deps dependencies) *cobra.Command {
	secretCmd := &cobra.Command{
		Use:   "secret",
		Short: "secret ファイル (~/.config/sbxr/secrets.env) を扱う",
	}
	setup := &cobra.Command{
		Use:   "setup",
		Short: "token を secret ファイルへ書く",
	}
	setup.AddCommand(newSecretSetupGithubCmd(deps), newSecretSetupCustomCmd(deps))
	secretCmd.AddCommand(setup)
	return secretCmd
}

func newSecretSetupGithubCmd(deps dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "github",
		Short: "GitHub の fine-grained PAT の能力を確かめてから secret ファイルへ書く",
		Long: `GitHub の fine-grained PAT の能力を確かめてから secret ファイルへ書く。
利用者が指定した private repo で、付けてはならない権限が拒否され、Contents を読めることを GitHub API で確かめる。
確かめられなければ書かない。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			def, err := loadSecretDef(deps, githubSecretName)
			if err != nil {
				return err
			}
			printf(cmd, `sandbox VM 専用の fine-grained PAT を作る (https://github.com/settings/personal-access-tokens/new)。
付ける権限 (Repository permissions):
  - Contents: Read and write
  - Issues: Read and write
  - Pull requests: Read and write
  - Metadata: Read-only (自動で付く)
付けない権限: 上に無いものすべて (Workflows を含む)。次は token が拒否されることを API で確かめる: %s
repo の削除と force push は token の権限では防げないので、branch protection で守る。

`, strings.Join(secret.ForbiddenPermissions(), " / "))
			repo, err := deps.prompter.Line("probe に使う private repo (owner/name。token が対象にし、commit が 1 つ以上あるもの): ")
			if err != nil {
				return err
			}
			repo = strings.TrimSpace(repo)
			if err := secret.ValidateGitHubRepo(repo); err != nil { // token を尋ねる前に止める
				return err
			}
			token, err := readSecretValue(deps, "token: ")
			if err != nil {
				return err
			}
			probe := secret.GitHubProbe{BaseURL: deps.githubAPI, Client: &http.Client{Timeout: 30 * time.Second}}
			if err := probe.Verify(cmd.Context(), repo, token); err != nil {
				return fmt.Errorf("token を secret ファイルへ書かなかった: %w", err)
			}
			return writeSecretValue(cmd, deps, def.Key, token)
		},
	}
}

func newSecretSetupCustomCmd(deps dependencies) *cobra.Command {
	var host string
	cmd := &cobra.Command{
		Use:   "custom --host <host>",
		Short: "host へ placeholder 注入する secret の値を、能力を確かめずに secret ファイルへ書く",
		Long: `host へ placeholder 注入する secret の値を、能力を確かめずに secret ファイルへ書く。
書くキーは、user 設定か同梱の secret 定義のうち hosts に host を持つものの key。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			key, err := secretKeyForHost(deps, host)
			if err != nil {
				return err
			}
			printf(cmd, "%s の値を %s として書く。token の能力は確認していない (付けた権限はすべて VM 内の agent が使える)。\n", host, key)
			value, err := readSecretValue(deps, "value: ")
			if err != nil {
				return err
			}
			return writeSecretValue(cmd, deps, key, value)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "注入先 host (secret 定義の hosts に書いたもの)")
	_ = cmd.MarkFlagRequired("host") // flag は直前で定義しているので失敗しない
	return cmd
}

func loadSecretDefs(deps dependencies) (map[string]secret.Definition, error) {
	userConfigPath, err := deps.userConfigPath()
	if err != nil {
		return nil, err
	}
	raw, err := config.LoadSecretDefs(userConfigPath)
	if err != nil {
		return nil, err
	}
	return secret.ParseDefinitions(raw)
}

func loadSecretDef(deps dependencies, name string) (secret.Definition, error) {
	defs, err := loadSecretDefs(deps)
	if err != nil {
		return secret.Definition{}, err
	}
	def, ok := defs[name]
	if !ok {
		return secret.Definition{}, fmt.Errorf("secret 定義 %s が無い", name)
	}
	return def, nil
}

// secretKeyForHost は host へ placeholder 注入する secret 定義の key を返す。候補が 1 つに決まらなければ止める。
func secretKeyForHost(deps dependencies, host string) (string, error) {
	defs, err := loadSecretDefs(deps)
	if err != nil {
		return "", err
	}
	keys := map[string][]string{} // key → それを使う定義の名前
	for _, name := range slices.Sorted(maps.Keys(defs)) {
		def := defs[name]
		if def.InjectsPlaceholder() && slices.Contains(def.Hosts, host) {
			keys[def.Key] = append(keys[def.Key], name)
		}
	}
	switch len(keys) {
	case 0:
		return "", fmt.Errorf("%s へ placeholder 注入する secret 定義が無い (user 設定の secret_defs に定義してから実行する)", host)
	case 1:
		for key := range keys {
			return key, nil
		}
	}
	return "", fmt.Errorf("%s へ注入する secret 定義の key が複数ある (%s)。1 つに揃える", host, strings.Join(slices.Sorted(maps.Keys(keys)), ", "))
}

func readSecretValue(deps dependencies, prompt string) (string, error) {
	value, err := deps.prompter.Hidden(prompt)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("値が空")
	}
	return value, nil
}

func writeSecretValue(cmd *cobra.Command, deps dependencies, key, value string) error {
	path, err := deps.secretFilePath()
	if err != nil {
		return err
	}
	if err := secret.WriteValue(path, key, value); err != nil {
		return err
	}
	printf(cmd, "%s に %s を書いた\n", path, key)
	return nil
}

// terminalPrompter は端末から入力を読む。prompt は stderr に出す。
type terminalPrompter struct {
	stdin *bufio.Reader
}

func newTerminalPrompter() terminalPrompter {
	return terminalPrompter{stdin: bufio.NewReader(os.Stdin)}
}

func (p terminalPrompter) Line(prompt string) (string, error) {
	_, _ = fmt.Fprint(os.Stderr, prompt)
	line, err := p.stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("入力を読めない: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Confirm は y か yes (大小文字を問わない) だけを承認として読む。端末でなければ止める (確認なしに進めない)。
func (p terminalPrompter) Confirm(prompt string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, errors.New("確認には端末が要る (stdin が端末でない)")
	}
	answer, err := p.Line(prompt)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// Hidden は golang.org/x/term で入力を画面に出さずに読む。端末でなければ止める (pipe の値を黙って受けない)。
// 入力中の Ctrl-C でも端末の echo を戻してから終わる。
func (p terminalPrompter) Hidden(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("値の入力には端末が要る (stdin が端末でない)")
	}
	state, err := term.GetState(fd)
	if err != nil {
		return "", fmt.Errorf("端末の状態を読めない: %w", err)
	}
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	defer func() {
		signal.Stop(interrupted)
		close(done)
	}()
	go func() {
		select {
		case <-interrupted:
			_ = term.Restore(fd, state)
			_, _ = fmt.Fprintln(os.Stderr)
			os.Exit(130)
		case <-done:
		}
	}()
	_, _ = fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("入力を読めない: %w", err)
	}
	return string(value), nil
}
