// Package sbxstub は sbx CLI の policy 操作と secret 操作を in-memory で再現する test 用の stub。
// runtime.Sbx にコマンド実行の代わりとして渡し、実 sbx の global rule と secret に触れずに検証する。
package sbxstub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Rule は sbx policy ls --json が返す rule のうち、sbxr が読む field。
// runtime.Sbx の decoder とは独立に sbx の出力形式を写したもので、両者の食い違いは adapter の test が検出する。
type Rule struct {
	ID           string   `json:"id"`
	Scope        string   `json:"scope"`
	ResourceType string   `json:"resource_type"`
	Decision     string   `json:"decision"`
	Resources    []string `json:"resources"`
	Editable     bool     `json:"editable"`
}

// Stub は sbx の policy の状態と、受け取った書き込みコマンドの記録を持つ。
type Stub struct {
	Rules []Rule
	// Writes は受け取った書き込みコマンド (policy allow / rm、secret set / set-custom。引数を空白で連結したもの)。
	Writes []string
	// Inputs は書き込みコマンドが stdin で受け取った内容 (Writes と同じ順。stdin が無ければ空文字)。
	Inputs []string
	// DropWrites が true なら書き込みを記録だけして状態に反映しない (適用が効かない sbx の再現)。
	DropWrites bool
	// FailOnWrite が n (1 始まり) なら n 回目の書き込みを失敗させる。0 なら失敗させない。
	FailOnWrite int
	// FailOn は、引数を空白で連結したものがこの前置きで始まるコマンドを失敗させる。空なら失敗させない。
	FailOn string
	// Sandboxes は sandbox の名前と status。env create で running になり、env rm で消える。
	Sandboxes map[string]string
	// SandboxRules は sandbox スコープ rule の宛先。sandbox と一緒に消える。
	SandboxRules map[string][]string
	// SandboxSecrets は sandbox スコープの secret の数。sandbox と一緒に消える。
	SandboxSecrets map[string]int
	// VM は sbx exec を受ける sandbox VM の中身。nil なら sbx exec は失敗する。
	VM     *FakeVM
	nextID int
}

// GlobalAllow は scope=global・network・editable の allow rule を作る。
func GlobalAllow(id string, resources ...string) Rule {
	return Rule{ID: id, Scope: "global", ResourceType: "network", Decision: "allow", Resources: resources, Editable: true}
}

// Run は sbx の引数を受け取り、policy ls / allow network / rm network と secret set / set-custom を再現する。
func (s *Stub) Run(_ context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	input := ""
	if stdin != nil {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, err
		}
		input = string(data)
	}
	if s.FailOn != "" && strings.HasPrefix(strings.Join(args, " "), s.FailOn) {
		return nil, fmt.Errorf("sbxstub: %q を失敗させた", s.FailOn)
	}
	switch {
	case len(args) >= 1 && args[0] == "exec":
		return s.exec(args[1:], input)
	case len(args) >= 2 && args[0] == "secret" && (args[1] == "set" || args[1] == "set-custom"):
		if err := s.recordWrite(args, input); err != nil {
			return nil, err
		}
		if index := slices.Index(args, "--sandbox"); index >= 0 && index+1 < len(args) {
			s.ensureMaps()
			s.SandboxSecrets[args[index+1]]++
		}
		return nil, nil
	case slices.Equal(args, []string{"ls", "--json"}):
		type sandbox struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		list := []sandbox{}
		for _, name := range slices.Sorted(maps.Keys(s.Sandboxes)) {
			list = append(list, sandbox{Name: name, Status: s.Sandboxes[name]})
		}
		return json.Marshal(map[string][]sandbox{"sandboxes": list})
	case len(args) == 4 && slices.Equal(args[:3], []string{"env", "create", "--auto-approve"}):
		name, err := envName(args[3])
		if err != nil {
			return nil, err
		}
		if err := s.recordWrite(args, input); err != nil {
			return nil, err
		}
		s.ensureMaps()
		s.Sandboxes[name] = "running"
		return nil, nil
	case len(args) == 4 && slices.Equal(args[:3], []string{"env", "rm", "--force"}):
		// 実 sbx と同じく、env 定義が無ければ消せない。sandbox が無くても sandbox スコープの secret は消して成功する
		name, err := envName(args[3])
		if err != nil {
			return nil, err
		}
		if err := s.recordWrite(args, input); err != nil {
			return nil, err
		}
		delete(s.Sandboxes, name)
		delete(s.SandboxRules, name)
		delete(s.SandboxSecrets, name)
		return nil, nil
	case len(args) == 2 && args[0] == "stop":
		if err := s.recordWrite(args, input); err != nil {
			return nil, err
		}
		if _, ok := s.Sandboxes[args[1]]; !ok {
			return nil, fmt.Errorf("sbxstub: sandbox %s が無い", args[1])
		}
		s.Sandboxes[args[1]] = "stopped"
		return nil, nil
	case len(args) == 6 && slices.Equal(args[:4], []string{"policy", "allow", "network", "--sandbox"}):
		// 実 sbx と同じく、sandbox の作成前には置けない
		if _, ok := s.Sandboxes[args[4]]; !ok {
			return nil, fmt.Errorf("sbxstub: sandbox %q not found", args[4])
		}
		if err := s.recordWrite(args, input); err != nil {
			return nil, err
		}
		s.ensureMaps()
		s.SandboxRules[args[4]] = append(s.SandboxRules[args[4]], args[5])
		return nil, nil
	case slices.Equal(args, []string{"policy", "ls", "--json"}):
		// sbx は rule が無くても空の配列を返す
		return json.Marshal(map[string][]Rule{"rules": append([]Rule{}, s.Rules...)})
	case len(args) == 4 && slices.Equal(args[:3], []string{"policy", "allow", "network"}):
		if err := s.recordWrite(args, input); err != nil {
			return nil, err
		}
		if s.DropWrites {
			return nil, nil
		}
		for _, resource := range strings.Split(args[3], ",") {
			s.nextID++
			s.Rules = append(s.Rules, GlobalAllow(fmt.Sprintf("added-%d", s.nextID), resource))
		}
		return nil, nil
	case len(args) == 5 && slices.Equal(args[:4], []string{"policy", "rm", "network", "--id"}):
		if err := s.recordWrite(args, input); err != nil {
			return nil, err
		}
		if s.DropWrites {
			return nil, nil
		}
		index := slices.IndexFunc(s.Rules, func(r Rule) bool { return r.ID == args[4] })
		if index < 0 {
			return nil, fmt.Errorf("sbxstub: rule %s が無い", args[4])
		}
		s.Rules = slices.Delete(s.Rules, index, index+1)
		return nil, nil
	}
	return nil, fmt.Errorf("sbxstub: 想定外の引数 %q", args)
}

// exec は sbx exec [-i] [-w dir] <sandbox> -- <args> を VM へ渡す。
func (s *Stub) exec(args []string, input string) ([]byte, error) {
	command := VMCommand{}
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-i":
			command.Input = &input
			args = args[1:]
		case "-w":
			command.Dir, args = args[1], args[2:]
		default:
			return nil, fmt.Errorf("sbxstub: exec の想定外のフラグ %q", args[0])
		}
	}
	if len(args) < 3 || args[1] != "--" {
		return nil, fmt.Errorf("sbxstub: exec の引数 %q が <sandbox> -- <command> の形でない", args)
	}
	if status, ok := s.Sandboxes[args[0]]; !ok || status != "running" {
		return nil, fmt.Errorf("sbxstub: sandbox %s が動いていない", args[0])
	}
	if s.VM == nil {
		return nil, fmt.Errorf("sbxstub: VM が無い")
	}
	command.Args = args[2:]
	return s.VM.Exec(command)
}

func (s *Stub) ensureMaps() {
	if s.Sandboxes == nil {
		s.Sandboxes = map[string]string{}
	}
	if s.SandboxRules == nil {
		s.SandboxRules = map[string][]string{}
	}
	if s.SandboxSecrets == nil {
		s.SandboxSecrets = map[string]int{}
	}
}

// envName は env 定義 (<dir>/sbxenv.yaml) の name を読む。
func envName(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "sbxenv.yaml"))
	if err != nil {
		return "", fmt.Errorf("sbxstub: no sbxenv.yaml found at %s: %w", dir, err)
	}
	var env struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil || env.Name == "" {
		return "", fmt.Errorf("sbxstub: %s/sbxenv.yaml の name を読めない", dir)
	}
	return env.Name, nil
}

func (s *Stub) recordWrite(args []string, input string) error {
	if s.FailOnWrite == len(s.Writes)+1 {
		return fmt.Errorf("sbxstub: %d 回目の書き込みを失敗させた", s.FailOnWrite)
	}
	s.Writes = append(s.Writes, strings.Join(args, " "))
	s.Inputs = append(s.Inputs, input)
	return nil
}
