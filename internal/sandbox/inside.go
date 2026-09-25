package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/runtime"
)

// VM 内の置き場。$HOME は VM の agent user の home で、実行時に読む。
const (
	settingsRelPath = ".claude/settings.json"
	// BootScriptRelPath は boot を書き出す場所。埋め込みの kit sbxr-boot と対。
	BootScriptRelPath = ".config/sbxr/boot.sh"
)

// writeFileScript は stdin を $1 へ書き、$2 があれば mode にする。ディレクトリが無ければ作る。
// sbx cp は host の uid と mode のまま置き、VM の agent から読めないことがあるので、VM 内の shell で書く。
const writeFileScript = `set -e; mkdir -p "$(dirname "$1")"; cat > "$1"; if [ -n "$2" ]; then chmod "$2" "$1"; fi`

// aptWait は init の前に VM 内の apt-get が終わるのを待つ上限と間隔。test が差し替える。
var aptWait = struct {
	budget, interval time.Duration
	sleep            func(time.Duration)
}{300 * time.Second, 2 * time.Second, time.Sleep}

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

func (v vm) readFile(ctx context.Context, rel string) ([]byte, error) {
	return v.run(ctx, runtime.SandboxCommand{Args: []string{"cat", v.path(rel)}})
}

func (v vm) writeFile(ctx context.Context, rel string, data []byte, mode string) error {
	args := []string{"sh", "-c", writeFileScript, "sh", v.path(rel)}
	if mode != "" {
		args = append(args, mode)
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

// StageError は create の後段 (VM を作った後) のどの段で失敗したかを持つ。
type StageError struct {
	Stage string
	Err   error
}

func (e *StageError) Error() string { return fmt.Sprintf("%s が失敗した: %v", e.Stage, e.Err) }
func (e *StageError) Unwrap() error { return e.Err }

func stageError(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &StageError{Stage: stage, Err: err}
}

// setUpInside は作った VM の中を宣言どおりにする: materialize → read-back → init → boot (create 時の 1 回)。
func setUpInside(ctx context.Context, rt runtime.Runtime, prepared Prepared, progress io.Writer) error {
	v, err := openVM(ctx, rt, prepared.Target.Name)
	if err != nil {
		return stageError("materialize", err)
	}
	decl := prepared.Declaration
	settings, err := profileSettings(decl.Profile)
	if err != nil {
		return stageError("materialize", err)
	}
	if err := stageError("materialize", materialize(ctx, v, settings, decl, prepared.OriginHost)); err != nil {
		return err
	}
	logf(progress, "materialize: settings.json に %d key、git identity (%s <%s>) を書いた\n", len(settings), decl.Git.Name, decl.Git.Email)
	if err := stageError("read-back", readBack(ctx, v, settings, decl)); err != nil {
		return err
	}
	if err := stageError("init", runInit(ctx, v, prepared.Target.Repo, decl.Init, progress)); err != nil {
		return err
	}
	return stageError("boot", runBoot(ctx, v, prepared.Target.Repo, decl.Boot, progress))
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
	return normalizeJSON(settings)
}

// normalizeJSON は JSON に通して、比較できる形 (数値は float64、map は map[string]any) に揃える。
func normalizeJSON(value map[string]any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(data, &out)
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
	if err := v.writeFile(ctx, settingsRelPath, append(merged, '\n'), ""); err != nil {
		return fmt.Errorf("settings.json を書けない: %w", err)
	}
	if err := installPlugins(ctx, v, decl.Profile); err != nil {
		return err
	}
	for key, value := range map[string]string{"user.name": decl.Git.Name, "user.email": decl.Git.Email} {
		if err := gitConfig(ctx, v, "--replace-all", key, value); err != nil {
			return err
		}
	}
	return rewriteSSHToHTTPS(ctx, v, originHost)
}

// readSettings は VM の settings.json を読む。無ければ空として扱う。
func readSettings(ctx context.Context, v vm) (map[string]any, error) {
	data, err := v.readFile(ctx, settingsRelPath)
	if err != nil {
		// cat の失敗は「無い」とそれ以外を区別できないので、無いものとして扱い、read-back で確かめる
		return map[string]any{}, nil
	}
	settings := map[string]any{}
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

func gitConfig(ctx context.Context, v vm, mode, key, value string) error {
	if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"git", "config", "--global", mode, key, value}}); err != nil {
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
	if err := gitConfig(ctx, v, "--replace-all", key, "git@"+host+":"); err != nil {
		return err
	}
	return gitConfig(ctx, v, "--add", key, "ssh://git@"+host+"/")
}

// installPlugins は enabledPlugins の plugin を install する。marketplace が VM に無ければ、
// extraKnownMarketplaces の宣言から足す (VM の Claude Code は enabledPlugins だけでは install しない)。
func installPlugins(ctx context.Context, v vm, profile config.Profile) error {
	for _, spec := range enabledPlugins(profile) {
		_, marketplace, _ := strings.Cut(spec, "@")
		if repo := marketplaceRepo(profile, marketplace); repo != "" {
			if err := ensureMarketplace(ctx, v, repo); err != nil {
				return err
			}
		}
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

func ensureMarketplace(ctx context.Context, v vm, repo string) error {
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
	if slices.ContainsFunc(listed, func(m struct {
		Repo string `json:"repo"`
	}) bool {
		return m.Repo == repo
	}) {
		return nil
	}
	if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"claude", "plugin", "marketplace", "add", repo}}); err != nil {
		return fmt.Errorf("plugin marketplace %s を足せない: %w", repo, err)
	}
	return nil
}

// readBack は materialize の結果を VM から読み戻して宣言と比べる。
// settings.json は profile の top-level key ごとに、宣言の値が VM の値に含まれるか (map は sbx が足した key を許す) を見る。
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
	for key, want := range map[string]string{"user.name": decl.Git.Name, "user.email": decl.Git.Email} {
		out, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"git", "config", "--global", "--get-all", key}})
		if err != nil || strings.TrimSpace(string(out)) != want {
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

// runInit は init を VM 内の repo root で 1 件ずつ実行する。1 件以上あるときだけ、先に VM 内の apt-get が終わるのを待つ
// (起動直後に sbx が background で走らせる apt と、init の apt が lock で衝突するのを避ける)。
func runInit(ctx context.Context, v vm, repo string, commands []string, progress io.Writer) error {
	if len(commands) == 0 {
		return nil
	}
	if !waitForApt(ctx, v) {
		logf(progress, "init: 警告 VM 内の apt-get が %s 経っても終わらない (init の apt が lock で失敗しうる)\n", aptWait.budget)
	}
	for i, command := range commands {
		logf(progress, "init[%d]: %s\n", i+1, command)
		if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"bash", "-c", command}, Dir: repo}); err != nil {
			return fmt.Errorf("init[%d] (%s): %w", i+1, command, err)
		}
	}
	return nil
}

// waitForApt は VM 内の apt-get が終わるまで待つ。上限までに終われば true。
// pgrep は apt-get が無いと 0 以外で終わるので、error を「動いていない」と読む。
func waitForApt(ctx context.Context, v vm) bool {
	for waited := time.Duration(0); waited < aptWait.budget; waited += aptWait.interval {
		if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"pgrep", "-x", "apt-get"}}); err != nil {
			return true
		}
		aptWait.sleep(aptWait.interval)
	}
	return false
}

// runBoot は boot を VM の boot script に書き、create 時の 1 回を実行する。2 回目以降の起動では kit sbxr-boot が実行する。
// 起動ごとに宣言を読み直さず、作成時に確定した内容を再生する。
func runBoot(ctx context.Context, v vm, repo string, commands []string, progress io.Writer) error {
	if len(commands) == 0 {
		return nil
	}
	if err := v.writeFile(ctx, BootScriptRelPath, []byte(bootScript(repo, commands)), "0755"); err != nil {
		return fmt.Errorf("boot script を書けない: %w", err)
	}
	logf(progress, "boot: %d 件を VM の ~/%s に書いた (起動ごとに実行する)\n", len(commands), BootScriptRelPath)
	out, err := v.run(ctx, runtime.SandboxCommand{Args: []string{v.path(BootScriptRelPath)}})
	logf(progress, "%s", out)
	return err
}

// bootScript は boot の各コマンドを repo root で順に実行する script。1 件の失敗で止めず、最後に失敗を返す
// (起動ごとの実行では host から見えないので、どの entry が失敗したかを log に残す)。
func bootScript(repo string, commands []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n# sbxr が create 時に書いた boot。kit sbxr-boot が sandbox VM の起動ごとに実行する。\n")
	fmt.Fprintf(&b, "rc=0\ncd %s || exit 1\n", shellQuote(repo))
	for i, command := range commands {
		fmt.Fprintf(&b, "echo 'boot[%d]: start'\n", i+1)
		fmt.Fprintf(&b, "bash -c %s < /dev/null || { echo \"boot[%d] fail exit=$?\"; rc=1; }\n", shellQuote(command), i+1)
	}
	b.WriteString("exit \"$rc\"\n")
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// originHost は host 側の repo の origin の host を返す。origin が無ければ空。
func originHost(ctx context.Context, repo string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "config", "--get", "remote.origin.url").Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 { // key が無い
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("repo %s の origin を読めない: %w", repo, err)
	}
	return gitURLHost(strings.TrimSpace(string(out))), nil
}
