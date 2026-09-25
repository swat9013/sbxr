package secret

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/swat9013/sbxr/internal/egress"
	"github.com/swat9013/sbxr/internal/runtime"
)

// Plan は 1 つの sandbox VM に配線する secret と、要求されたが配線しない secret。
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

// Apply は計画した secret を sandbox VM に限った secret として実行基盤に置く。
// 値がすべて secret ファイルにあることを確かめてから書き込む (一部だけ配線された VM を作らない)。
func Apply(ctx context.Context, rt runtime.Runtime, sandbox string, plan Plan, values Values) error {
	secrets := make([]runtime.SandboxSecret, 0, len(plan.Wired))
	var errs []error
	for _, wire := range plan.Wired {
		value, ok := values[wire.Definition.Key]
		if !ok || value == "" {
			errs = append(errs, fmt.Errorf("secret %s: secret ファイルに %s が無い (sbxr secret setup で書く)", wire.Name, wire.Definition.Key))
			continue
		}
		secrets = append(secrets, runtime.SandboxSecret{
			Service: wire.Definition.Service,
			Hosts:   wire.Definition.Hosts,
			Env:     wire.Definition.Env,
			Value:   value,
		})
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	for i, secret := range secrets {
		if err := rt.SetSandboxSecret(ctx, sandbox, secret); err != nil {
			return fmt.Errorf("secret %s を配線できない: %w", plan.Wired[i].Name, err)
		}
	}
	return nil
}
