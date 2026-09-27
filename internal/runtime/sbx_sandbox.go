package runtime

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/assets"
)

// sbx の env 定義と kit は、sbx v0.45.1 の `sbx env` に合わせた約束 (decision/0009)。

// envFile は状態ディレクトリに置く env 定義のファイル名。
const envFile = "sbxenv.yaml"

// envDefinition は sbx env create に渡す env 定義。repo は VM 内の clone として渡す (host の作業ツリーを書き換えさせない)。
// agent は Claude Code に固定する (agent runtime profile は Claude Code の settings.json だけを扱う)。
type envDefinition struct {
	SchemaVersion string            `yaml:"schemaVersion"`
	Agent         string            `yaml:"agent"`
	Name          string            `yaml:"name"`
	Workspace     envWorkspace      `yaml:"workspace"`
	Kits          []envKit          `yaml:"kits"`
	Env           map[string]string `yaml:"env,omitempty"`
}

type envWorkspace struct {
	Path  string `yaml:"path"`
	Clone bool   `yaml:"clone"`
}

// envKit は env 定義の kits の 1 要素。
type envKit struct {
	Source string            `yaml:"source"`
	Args   map[string]string `yaml:"args,omitempty"`
}

// 埋め込みの kit の名前 (internal/assets/kits の下のディレクトリ名)。
const (
	bootKit  = "sbxr-boot"
	herdrKit = "sbxr-herdr"
)

// kitsDir は状態ディレクトリの中で埋め込みの kit を置くディレクトリ。env 定義からは相対 path で指す
// (sbx は ./ で始まる kit を env 定義のディレクトリ基準で解決する)。
const kitsDir = "kits"

// embeddedKit は env 定義に入れる埋め込みの kit。
type embeddedKit struct {
	name string
	args map[string]string
}

// source は env 定義から kit を指す相対 path。
func (k embeddedKit) source() string { return "./" + kitsDir + "/" + k.name }

// kits は env 定義に入れる埋め込みの kit。boot の再生は常に入れ、herdr は導入するときだけ入れる。
// herdr を先に置く (順序の理由は ADR 0007)。
func kits(spec SandboxSpec) []embeddedKit {
	var list []embeddedKit
	if spec.Herdr != nil {
		list = append(list, embeddedKit{name: herdrKit, args: map[string]string{"version": spec.Herdr.Version}})
	}
	return append(list, embeddedKit{name: bootKit})
}

// DefineSandbox は状態ディレクトリ (sbxr が作ったもの) に env 定義と埋め込みの kit を書く。
func (s *Sbx) DefineSandbox(stateDir string, spec SandboxSpec) error {
	selected := kits(spec)
	var refs []envKit
	for _, kit := range selected {
		refs = append(refs, envKit{Source: kit.source(), Args: kit.args})
	}
	env, err := yaml.Marshal(envDefinition{
		SchemaVersion: "1",
		Agent:         "claude",
		Name:          spec.Name,
		Workspace:     envWorkspace{Path: spec.Repo, Clone: true},
		Kits:          refs,
		Env:           spec.Env,
	})
	if err != nil {
		return err
	}
	// CopyFS は既存のファイルを上書きしないので、前回の残りを消してから書く
	if err := os.RemoveAll(filepath.Join(stateDir, kitsDir)); err != nil {
		return fmt.Errorf("状態ディレクトリの kit を書き直せない: %w", err)
	}
	for _, kit := range selected {
		sub, err := fs.Sub(assets.Kits(), kit.name)
		if err == nil {
			err = os.CopyFS(filepath.Join(stateDir, kitsDir, kit.name), sub)
		}
		if err != nil {
			return fmt.Errorf("状態ディレクトリに kit %s を書けない: %w", kit.name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(stateDir, envFile), env, 0o600); err != nil {
		return fmt.Errorf("状態ディレクトリに %s を書けない: %w", envFile, err)
	}
	return nil
}

// CreateSandbox は sbx の順序の制約 (ADR 0006 の実測) どおりに作る: sandbox スコープの secret は作成前に置き
// (作成時に VM の環境変数へ placeholder が入る)、sandbox スコープ rule は作成後に足す (作成前には置けない)。
// herdr を導入するなら、kit が settings.json に integration を書き終えるまで待つ (後段の書き込みと競合させない)。
// secret を置く途中で失敗したら、置いた分は残る。sandbox スコープの secret なので destroy で消える。
func (s *Sbx) CreateSandbox(ctx context.Context, stateDir string, spec SandboxSpec) error {
	for i, secret := range spec.Secrets {
		if err := s.setSandboxSecret(ctx, spec.Name, secret); err != nil {
			return fmt.Errorf("sandbox スコープの secret を置けない (%d 件目。置いた分は sandbox VM の destroy で消える): %w", i+1, err)
		}
	}
	if err := s.createEnvironment(ctx, stateDir); err != nil {
		return fmt.Errorf("sandbox VM %s を作れない: %w", spec.Name, err)
	}
	for _, resource := range spec.EgressRules {
		if err := s.allowSandboxEgress(ctx, spec.Name, resource); err != nil {
			return &CreatedError{Step: CreatedStepSandboxEgress, Err: fmt.Errorf("%s を足せない: %w", resource, err)}
		}
	}
	if spec.Herdr != nil {
		if err := s.waitStartup(ctx, spec.Name); err != nil {
			return &CreatedError{Step: CreatedStepStartup, Err: err}
		}
	}
	return nil
}

// DefinedWithHerdr は状態ディレクトリの env 定義に herdr の kit があるかを返す。
func (s *Sbx) DefinedWithHerdr(stateDir string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, envFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var env envDefinition
	if err := yaml.Unmarshal(data, &env); err != nil {
		return false, fmt.Errorf("状態ディレクトリの %s を読めない: %w", envFile, err)
	}
	herdrSource := embeddedKit{name: herdrKit}.source()
	for _, kit := range env.Kits {
		if kit.Source == herdrSource {
			return true, nil
		}
	}
	return false, nil
}

// kitStartupLog は sbx が VM 内に kit startup の経過を書く log (VM の /etc/durable-startup.d/run.sh が決める)。
const kitStartupLog = "/var/log/sbx-kit-startup.log"

// StartupWait は kit startup の完了を待つ上限と間隔。Sleep は test が差し替える。
// 上限は herdr の release の取得と server の起動を含む。sbx exec にかかる時間は数えないので、実際の待ちは上限より長くなりうる。
var StartupWait = struct {
	Budget, Interval time.Duration
	Sleep            func(time.Duration)
}{300 * time.Second, 2 * time.Second, time.Sleep}

// waitStartup は VM の起動時の kit startup が終わるまで待つ。失敗 (fail 行) か上限で error。
// kit startup の失敗は host からは見えないので、create 時はここで見る。
func (s *Sbx) waitStartup(ctx context.Context, sandbox string) error {
	var readErr error
	for waited := time.Duration(0); waited < StartupWait.Budget; waited += StartupWait.Interval {
		if err := ctx.Err(); err != nil {
			return err
		}
		var out []byte
		out, readErr = s.ReadSandboxFile(ctx, sandbox, kitStartupLog) // log は startup の途中まで無い
		switch startupOutcome(string(out)) {
		case startupComplete:
			return nil
		case startupFailed:
			return fmt.Errorf("VM の kit startup が失敗した (sbx exec %s -- cat %s で確かめる)", sandbox, kitStartupLog)
		}
		StartupWait.Sleep(StartupWait.Interval)
	}
	if readErr != nil {
		return fmt.Errorf("VM の kit startup が %s の間に終わらない (最後の log の読み取り: %w)", StartupWait.Budget, readErr)
	}
	return fmt.Errorf("VM の kit startup が %s の間に終わらない (sbx exec %s -- tail %s で確かめる)", StartupWait.Budget, sandbox, kitStartupLog)
}

type startup int

const (
	startupRunning startup = iota
	startupComplete
	startupFailed
)

// herdrKitFailure は kit sbxr-herdr が段の失敗を log に残す行の頭。kit は後段の boot を止めないよう、失敗しても 0 で終わる。
const herdrKitFailure = "sbxr-herdr: fail "

// startupOutcome は kit startup の log の最後の実行から、完了・失敗・実行中を読む。
// dispatcher の行の文面は sbx v0.45.1 の VM の /etc/durable-startup.d/run.sh に合わせている
// (段の失敗は "fail <script> exit=<N>" で、そこで止まる)。
func startupOutcome(log string) startup {
	if i := strings.LastIndex(log, "=== dispatcher run"); i >= 0 {
		log = log[i:]
	}
	for _, line := range strings.Split(log, "\n") {
		dispatcherFail := strings.HasPrefix(line, "fail /etc/durable-startup.d/") && strings.Contains(line, " exit=")
		if dispatcherFail || strings.HasPrefix(line, herdrKitFailure) {
			return startupFailed
		}
	}
	if strings.Contains(log, "=== dispatcher complete ===") {
		return startupComplete
	}
	return startupRunning
}
