package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/runtime"
)

// settingsRelPath は VM の agent user の home からの Claude Code の settings.json。
const settingsRelPath = ".claude/settings.json"

// writeFileScript は stdin を $1 へ書き、$2 があれば mode にする。ディレクトリが無ければ作る。
// sbx cp ではなく VM 内の shell で書く理由は ADR 0006 (VM の agent から読めない uid・mode で置かれる)。
const writeFileScript = `set -e; mkdir -p "$(dirname "$1")"; cat > "$1"; if [ -n "$2" ]; then chmod "$2" "$1"; fi`

// vm は 1 つの sandbox VM の中を操作する。
type vm struct {
	rt   runtime.Runtime
	name string
	home string
}

func (v vm) run(ctx context.Context, command runtime.SandboxCommand) ([]byte, error) {
	return v.rt.ExecInSandbox(ctx, v.name, command)
}

func (v vm) path(rel string) string {
	return v.home + "/" + rel
}

// exists は VM 内に rel があるかを返す。test -e の非 0 を「無い」と読むので、exec の失敗と区別できない。
// 読み取り (cat) の失敗は別に error として扱い、「無い」への黙った読み替えにはしない。
func (v vm) exists(ctx context.Context, rel string) bool {
	_, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"test", "-e", v.path(rel)}})
	return err == nil
}

func (v vm) readFile(ctx context.Context, rel string) ([]byte, error) {
	return v.run(ctx, runtime.SandboxCommand{Args: []string{"cat", v.path(rel)}})
}

// writeFile は rel に data を書く。mode が 0 なら mode を変えない。
func (v vm) writeFile(ctx context.Context, rel string, data []byte, mode fs.FileMode) error {
	args := []string{"sh", "-c", writeFileScript, "sh", v.path(rel)}
	if mode != 0 {
		args = append(args, fmt.Sprintf("%04o", mode.Perm()))
	}
	_, err := v.run(ctx, runtime.SandboxCommand{Args: args, Input: data})
	return err
}

func openVM(ctx context.Context, rt runtime.Runtime, name string) (vm, error) {
	out, err := rt.ExecInSandbox(ctx, name, runtime.SandboxCommand{Args: []string{"printenv", "HOME"}})
	if err != nil {
		return vm{}, fmt.Errorf("VM の home を読めない: %w", err)
	}
	home := strings.TrimSpace(string(out))
	if !strings.HasPrefix(home, "/") {
		return vm{}, fmt.Errorf("VM の home %q が絶対 path でない", home)
	}
	return vm{rt: rt, name: name, home: home}, nil
}

// Stage は create のうち、sandbox VM を作った後の段。
type Stage string

const (
	StageSandboxEgress Stage = "sandbox スコープ rule"
	StageMaterialize   Stage = "materialize"
	StageReadBack      Stage = "read-back"
	StageInit          Stage = "init"
	StageBoot          Stage = "boot"
)

// StageError は sandbox VM を作った後のどの段で失敗したかを持つ。この error のとき VM は残っている。
type StageError struct {
	Stage Stage
	Err   error
}

func (e *StageError) Error() string { return fmt.Sprintf("%s が失敗した: %v", e.Stage, e.Err) }
func (e *StageError) Unwrap() error { return e.Err }

func stageError(stage Stage, err error) error {
	if err == nil {
		return nil
	}
	return &StageError{Stage: stage, Err: err}
}

// setUpInside は作った VM の中を宣言どおりにする: materialize → read-back → init → boot (create 時の 1 回)。
func setUpInside(ctx context.Context, rt runtime.Runtime, prepared Prepared, progress io.Writer) error {
	v, err := openVM(ctx, rt, prepared.Target.Name)
	if err != nil {
		return stageError(StageMaterialize, err)
	}
	decl := prepared.Declaration
	settings, err := profileSettings(decl.Profile)
	if err != nil {
		return stageError(StageMaterialize, err)
	}
	if err := stageError(StageMaterialize, materialize(ctx, v, settings, decl, prepared.OriginHost)); err != nil {
		return err
	}
	logf(progress, "materialize: settings.json に %d key、git identity (%s <%s>) を書いた\n", len(settings), decl.Git.Name, decl.Git.Email)
	if err := stageError(StageReadBack, readBack(ctx, v, settings, decl)); err != nil {
		return err
	}
	if err := stageError(StageInit, runInit(ctx, v, prepared.Target.Repo, decl.Init, progress)); err != nil {
		return err
	}
	return stageError(StageBoot, runBoot(ctx, v, prepared.Target.Repo, decl.Boot, progress))
}

func logf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// profileSettings は profile を settings.json の key と値にする。宣言の無い key (nil・空の map) は入れない。
func profileSettings(profile config.Profile) (map[string]any, error) {
	data, err := yaml.Marshal(profile)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	settings := map[string]any{}
	for key, value := range raw {
		if m, isMap := value.(map[string]any); value == nil || (isMap && len(m) == 0) {
			continue
		}
		settings[key] = value
	}
	// JSON に通して、VM から読んだ値と比べられる形 (数値は float64、map は map[string]any) に揃える
	normalized, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(normalized, &out)
}

// gitIdentity は VM の git config に書く identity。
func gitIdentity(decl Declaration) map[string]string {
	return map[string]string{"user.name": decl.Git.Name, "user.email": decl.Git.Email}
}

// materialize は agent runtime profile (settings.json への merge と plugin) と git identity を VM へ書く。
// settings.json は sbx が初期値を置いているので、上書きせずに再帰的に merge する。
func materialize(ctx context.Context, v vm, settings map[string]any, decl Declaration, originHost string) error {
	current, err := readSettings(ctx, v)
	if err != nil {
		return err
	}
	merged, err := json.MarshalIndent(mergeSettings(current, settings), "", "  ")
	if err != nil {
		return err
	}
	if err := v.writeFile(ctx, settingsRelPath, append(merged, '\n'), 0); err != nil {
		return fmt.Errorf("settings.json を書けない: %w", err)
	}
	if err := installPlugins(ctx, v, decl.Profile); err != nil {
		return err
	}
	for key, value := range gitIdentity(decl) {
		if err := setGitConfig(ctx, v, key, value); err != nil {
			return err
		}
	}
	return rewriteSSHToHTTPS(ctx, v, originHost)
}

// readSettings は VM の settings.json を読む。無ければ空。あるのに読めなければ error (初期値を消した上書きをしない)。
func readSettings(ctx context.Context, v vm) (map[string]any, error) {
	settings := map[string]any{}
	if !v.exists(ctx, settingsRelPath) {
		return settings, nil
	}
	data, err := v.readFile(ctx, settingsRelPath)
	if err != nil {
		return nil, fmt.Errorf("VM の settings.json を読めない: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("VM の settings.json を読めない: %w", err)
	}
	return settings, nil
}

// mergeSettings は upper を lower に再帰的に重ねる (map は key ごと、それ以外は upper が勝つ)。
func mergeSettings(lower, upper map[string]any) map[string]any {
	out := maps.Clone(lower)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range upper {
		lowerMap, lowerIsMap := out[key].(map[string]any)
		upperMap, upperIsMap := value.(map[string]any)
		if lowerIsMap && upperIsMap {
			out[key] = mergeSettings(lowerMap, upperMap)
			continue
		}
		out[key] = value
	}
	return out
}

// setGitConfig は key の値を 1 つにする (既存の値をすべて置き換える。再実行しても同じ値になる)。
func setGitConfig(ctx context.Context, v vm, key, value string) error {
	return gitConfig(ctx, v, "--replace-all", key, value)
}

// addGitConfig は key に値を 1 つ足す。
func addGitConfig(ctx context.Context, v vm, key, value string) error {
	return gitConfig(ctx, v, "--add", key, value)
}

func gitConfig(ctx context.Context, v vm, flag, key, value string) error {
	if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"git", "config", "--global", flag, key, value}}); err != nil {
		return fmt.Errorf("VM の git config %s を書けない: %w", key, err)
	}
	return nil
}

// rewriteSSHToHTTPS は origin の host への ssh 形 (git@host: と ssh://git@host/) を https に書き換える。
// VM には ssh 鍵が無く、token は HTTPS にだけ注入されるため。
func rewriteSSHToHTTPS(ctx context.Context, v vm, host string) error {
	if host == "" {
		return nil
	}
	key := "url.https://" + host + "/.insteadOf"
	if err := setGitConfig(ctx, v, key, "git@"+host+":"); err != nil {
		return err
	}
	return addGitConfig(ctx, v, key, "ssh://git@"+host+"/")
}

// installPlugins は enabledPlugins の plugin を install する。marketplace が VM に無ければ、
// extraKnownMarketplaces の宣言から先に足す (VM の Claude Code は enabledPlugins だけでは install しない)。
func installPlugins(ctx context.Context, v vm, profile config.Profile) error {
	specs := enabledPlugins(profile)
	if len(specs) == 0 {
		return nil
	}
	if err := addMissingMarketplaces(ctx, v, profile, specs); err != nil {
		return err
	}
	for _, spec := range specs {
		if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"claude", "plugin", "install", spec}}); err != nil {
			return fmt.Errorf("plugin %s を install できない: %w", spec, err)
		}
	}
	return nil
}

func enabledPlugins(profile config.Profile) []string {
	var specs []string
	for _, spec := range slices.Sorted(maps.Keys(profile.EnabledPlugins)) {
		if profile.EnabledPlugins[spec] {
			specs = append(specs, spec)
		}
	}
	return specs
}

// marketplaceRepo は extraKnownMarketplaces に宣言した marketplace の GitHub repo (owner/name) を返す。無ければ空。
func marketplaceRepo(profile config.Profile, marketplace string) string {
	entry, _ := profile.ExtraKnownMarketplaces[marketplace].(map[string]any)
	source, _ := entry["source"].(map[string]any)
	repo, _ := source["repo"].(string)
	return repo
}

// addMissingMarketplaces は plugin の marketplace のうち、宣言に repo があり VM に無いものを足す。
func addMissingMarketplaces(ctx context.Context, v vm, profile config.Profile, specs []string) error {
	out, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"claude", "plugin", "marketplace", "list", "--json"}})
	if err != nil {
		return fmt.Errorf("VM の plugin marketplace を読めない: %w", err)
	}
	var listed []struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		return fmt.Errorf("claude plugin marketplace list --json の出力を読めない: %w", err)
	}
	present := map[string]bool{}
	for _, marketplace := range listed {
		present[marketplace.Repo] = true
	}
	for _, spec := range specs {
		_, marketplace, _ := strings.Cut(spec, "@")
		repo := marketplaceRepo(profile, marketplace)
		if repo == "" || present[repo] {
			continue
		}
		if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"claude", "plugin", "marketplace", "add", repo}}); err != nil {
			return fmt.Errorf("plugin marketplace %s を足せない: %w", repo, err)
		}
		present[repo] = true
	}
	return nil
}

// readBack は materialize の結果を VM から読み戻して宣言と比べる。
// settings.json は profile の top-level key ごとに、宣言の値が VM の値に含まれるか (map は sbx が足した key を許す) を見る。
// 読めなかったときは不一致ではなく error で返す。
func readBack(ctx context.Context, v vm, settings map[string]any, decl Declaration) error {
	var mismatches []string
	current, err := readSettings(ctx, v)
	if err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(settings)) {
		if !contains(current[key], settings[key]) {
			mismatches = append(mismatches, "settings.json の "+key)
		}
	}
	for key, want := range gitIdentity(decl) {
		out, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"git", "config", "--global", "--get-all", key}})
		if err != nil {
			return fmt.Errorf("VM の git config %s を読めない: %w", key, err)
		}
		if strings.TrimSpace(string(out)) != want {
			mismatches = append(mismatches, "git の "+key)
		}
	}
	installed, err := installedPlugins(ctx, v)
	if err != nil {
		return err
	}
	for _, spec := range enabledPlugins(decl.Profile) {
		if !slices.Contains(installed, spec) {
			mismatches = append(mismatches, "plugin "+spec)
		}
	}
	if len(mismatches) > 0 {
		slices.Sort(mismatches)
		return fmt.Errorf("VM から読み戻した値が宣言と一致しない: %s", strings.Join(mismatches, ", "))
	}
	return nil
}

// contains は want が got に含まれるか (map は want の key がすべて got にあり、値も含まれるか) を返す。
// map 以外 (list を含む) は一致で比べる。
func contains(got, want any) bool {
	wantMap, wantIsMap := want.(map[string]any)
	if !wantIsMap {
		return reflect.DeepEqual(got, want)
	}
	gotMap, gotIsMap := got.(map[string]any)
	if !gotIsMap {
		return false
	}
	for key, value := range wantMap {
		if !contains(gotMap[key], value) {
			return false
		}
	}
	return true
}

func installedPlugins(ctx context.Context, v vm) ([]string, error) {
	out, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"claude", "plugin", "list", "--json"}})
	if err != nil {
		return nil, fmt.Errorf("VM の plugin を読めない: %w", err)
	}
	var listed []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		return nil, fmt.Errorf("claude plugin list --json の出力を読めない: %w", err)
	}
	ids := make([]string, 0, len(listed))
	for _, plugin := range listed {
		ids = append(ids, plugin.ID)
	}
	return ids, nil
}
