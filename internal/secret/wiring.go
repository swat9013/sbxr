package secret

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/swat9013/sbxr/internal/egress"
	"github.com/swat9013/sbxr/internal/runtime"
)

// Plan は 1 つの sandbox VM に配線する secret と、要求されたが配線しない secret。
// 配線の結果として 3 つの形を出す (decision/0008): VM の環境変数 (VMEnv)、実行基盤に置く secret (SandboxSecrets)、
// 作成時の宣言に残す形 (WiredSecrets。Wired を値抜きで写したもの)。create と drift は field を選ばずにこれを使う。
type Plan struct {
	Wired   []Wire
	Skipped []Skip
}

// Wire は配線する secret。
type Wire struct {
	Name       string
	Definition Definition
}

// Skip は要求されたが、注入先 host が egress で許可されていないので配線しない secret。
type Skip struct {
	Name        string
	DeniedHosts []string
}

// PlanWiring は secret 要求を配線の計画にする。配線するのは「要求あり かつ 注入先 host がすべて egress で許可」のものだけ。
// allowed は sandbox VM に効く egress の宛先 (global rule と sandbox スコープ rule を合わせたもの)。
// 要求された名前に定義が無ければ、足りない名前をすべて挙げて error にする。
func PlanWiring(requested []string, defs map[string]Definition, allowed []string) (Plan, error) {
	var missing []string
	for _, name := range requested {
		if _, ok := defs[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return Plan{}, fmt.Errorf("secret 要求に定義が無い: %s (user 設定の secret_defs に定義する)", strings.Join(missing, ", "))
	}
	var plan Plan
	for _, name := range requested {
		def := defs[name]
		var denied []string
		for _, host := range def.Hosts {
			if !egress.AllowsHTTPS(allowed, host) {
				denied = append(denied, host)
			}
		}
		if len(denied) > 0 {
			plan.Skipped = append(plan.Skipped, Skip{Name: name, DeniedHosts: denied})
			continue
		}
		plan.Wired = append(plan.Wired, Wire{Name: name, Definition: def})
	}
	return plan, nil
}

// VMEnv は配線する secret の付随値 (vars) を 1 つの環境変数の集合にまとめる。VM の環境変数へ入れるのは create (#5) の担当。
// 同じ名前を違う値で持つ secret が 2 つあれば止める。
func (p Plan) VMEnv() (map[string]string, error) {
	env := map[string]string{}
	owner := map[string]string{}
	var errs []error
	for _, wire := range p.Wired {
		for _, name := range slices.Sorted(maps.Keys(wire.Definition.Vars)) {
			value := wire.Definition.Vars[name]
			if previous, ok := env[name]; ok && previous != value {
				errs = append(errs, fmt.Errorf("vars の %s を secret %s と %s が違う値で持つ", name, owner[name], wire.Name))
				continue
			}
			env[name] = value
			owner[name] = wire.Name
		}
	}
	return env, errors.Join(errs...)
}

// RequireValues は配線する secret の値がすべて secret ファイルにあるかを確かめ、足りない key をすべて挙げて error にする。
// create は状態ディレクトリを書く前にこれで止める (何も残さずに止まるため)。
func (p Plan) RequireValues(values Values) error {
	var errs []error
	for _, wire := range p.Wired {
		if values[wire.Definition.Key] == "" {
			errs = append(errs, fmt.Errorf("secret %s: secret ファイルに %s が無い (sbxr secret setup で書く)", wire.Name, wire.Definition.Key))
		}
	}
	return errors.Join(errs...)
}

// SandboxSecrets は配線する secret を、実行基盤に置く sandbox スコープの secret にする。値は secret ファイルの key から引く。
// 値がすべて secret ファイルにあることを確かめてから返す。
func (p Plan) SandboxSecrets(values Values) ([]runtime.SandboxSecret, error) {
	if err := p.RequireValues(values); err != nil {
		return nil, err
	}
	secrets := make([]runtime.SandboxSecret, 0, len(p.Wired))
	for _, wire := range p.Wired {
		secrets = append(secrets, runtime.SandboxSecret{
			Service: wire.Definition.Service,
			Hosts:   wire.Definition.Hosts,
			Env:     wire.Definition.Env,
			Value:   values[wire.Definition.Key],
		})
	}
	return secrets, nil
}

// WiredSecret は配線した secret の、作成時の宣言に残す形。secret 定義のうち値を除くすべてで、name 以外はどれも create で VM に焼かれる
// (service・hosts・env は sandbox スコープの secret、key は置く値、vars は VM の環境変数)。name は記録どうしを突き合わせる識別子。
type WiredSecret struct {
	Name    string   `yaml:"name"`
	Service string   `yaml:"service,omitempty"`
	Hosts   []string `yaml:"hosts"`
	Env     string   `yaml:"env,omitempty"`
	// Key は v0.1.0 の記録には無い。secret 定義の key は空にならないので、空なら v0.1.0 の記録。
	Key  string            `yaml:"key,omitempty"`
	Vars map[string]string `yaml:"vars,omitempty"`
}

// WiredSecrets は配線する secret を、作成時の宣言に残す形にする。
func (p Plan) WiredSecrets() []WiredSecret {
	var wired []WiredSecret
	for _, wire := range p.Wired {
		def := wire.Definition
		wired = append(wired, WiredSecret{Name: wire.Name, Service: def.Service, Hosts: def.Hosts, Env: def.Env, Key: def.Key, Vars: def.Vars})
	}
	return wired
}

// Comparable は作成時の記録と現在の配線を、差を比べられる形に揃える。secret の並びと注入先 host の並びは VM に効かないので並べ替える。
// key を記録していない (v0.1.0 の) secret は、現在の側の key と vars も外して比べない。notCompared は比べなかったことの説明。
func Comparable(recorded, current []WiredSecret) (before, after []WiredSecret, notCompared []string) {
	before = canonical(recorded)
	unrecorded := map[string]bool{}
	for _, record := range before {
		if record.Key == "" {
			unrecorded[record.Name] = true
			notCompared = append(notCompared, fmt.Sprintf("secret %s の key・vars は作成時に記録していないので比べていない", record.Name))
		}
	}
	after = canonical(current)
	for i := range after {
		if unrecorded[after[i].Name] {
			after[i].Key, after[i].Vars = "", nil
		}
	}
	return before, after, notCompared
}

func canonical(records []WiredSecret) []WiredSecret {
	records = slices.Clone(records)
	for i := range records {
		records[i].Hosts = slices.Sorted(slices.Values(records[i].Hosts))
	}
	slices.SortFunc(records, func(a, b WiredSecret) int { return strings.Compare(a.Name, b.Name) })
	return records
}

// Apply は計画した secret を sandbox VM に限った secret として実行基盤に置く。
// 値がすべて secret ファイルにあることを確かめてから書き込む (値が足りないまま一部だけ置くことはしない)。
// 実行基盤への書き込みが途中で失敗したら、置いた分は残る。sandbox スコープの secret なので destroy で消える。
func Apply(ctx context.Context, rt runtime.Runtime, sandbox string, plan Plan, values Values) error {
	secrets, err := plan.SandboxSecrets(values)
	if err != nil {
		return err
	}
	for i, secret := range secrets {
		if err := rt.SetSandboxSecret(ctx, sandbox, secret); err != nil {
			placed := make([]string, 0, i)
			for _, wire := range plan.Wired[:i] {
				placed = append(placed, wire.Name)
			}
			return fmt.Errorf("secret %s を配線できない (配線済み: %s。sandbox VM の destroy で消える): %w",
				plan.Wired[i].Name, strings.Join(placed, ", "), err)
		}
	}
	return nil
}
