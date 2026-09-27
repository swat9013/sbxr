package sbxstub

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// VMCommand は sbx exec が VM へ渡すコマンド。
type VMCommand struct {
	Args []string
	Dir  string
	// Input は -i で渡した stdin。-i が無ければ nil。
	Input *string
}

// ShellRun は VM 内で bash -c で走ったコマンドと、その作業ディレクトリ。
type ShellRun struct {
	Dir     string
	Command string
}

// FakeVM は sbxr が sbx exec で使うコマンドだけを再現する VM。
type FakeVM struct {
	// Files は VM 内のファイル (path → 内容)。
	Files map[string]string
	// Modes は書き込み時に指定された mode (path → "0755" 等)。
	Modes map[string]string
	// GitConfig は git config --global の値 (key → 値の並び)。
	GitConfig map[string][]string
	// Marketplaces は追加済みの plugin marketplace の repo (owner/name)。
	Marketplaces []string
	// Plugins は install 済みの plugin (name@marketplace)。
	Plugins []string
	// ShellRuns は bash -c で走ったコマンド。
	ShellRuns []ShellRun
	// Executed は path で直接実行されたファイル (boot script など)。
	Executed []string
	// FailShell はこの文字列の bash -c コマンドを失敗させる。
	FailShell string
	// FailExecute はこの path のファイルの実行を失敗させる。
	FailExecute string
	// FailRead が true なら cat を失敗させる (ファイルはあるのに読めない状態の再現)。
	FailRead bool
	// DropWrites が true なら書き込みを受け付けたふりをして Files に反映しない (read-back の不一致の再現)。
	DropWrites bool
	// AptBusyPolls は pgrep -x apt-get が「動いている」と答える回数。
	AptBusyPolls int
	// AptPolls は pgrep -x apt-get が呼ばれた回数。
	AptPolls int
	// HTTP は VM 内から https://<host>/ へ送ったときの結果 (host → 結果)。無い host は DefaultHTTP で決まる。
	HTTP map[string]HTTPResult
	// Events は bash -c の実行 ("shell <command>")・ファイルの実行 ("exec <path>")・herdr server の停止 ("stop herdr server")・
	// https の probe ("probe <url>") を起きた順に並べる。
	Events []string
}

// HTTPResult は VM 内の curl が 1 つの宛先から得た結果。
type HTTPResult struct {
	// Code は状態コード。空なら HTTP 応答が無い (curl の失敗)。
	Code string
	// Body は応答の body。Code が空なら curl の error。
	Body string
}

// ProxyDenial は sbx の proxy の拒否応答 (sbx v0.45.1 の実測)。
func ProxyDenial(host string) HTTPResult {
	return HTTPResult{Code: "403", Body: "Blocked by network policy: domain " + host + ":443\ndetail: no matching allow rule — blocked by default deny policy\n"}
}

// DefaultHTTP は HTTP に無い host の結果: IANA の予約 domain そのもの (example.com など。subdomain は含めない) は proxy が拒否し、
// それ以外は 200 を返す。
// 既定の宣言で作った VM の egress 自己検証が通る状態 (global policy が default deny で、global rule が宣言どおり) を再現する。
func DefaultHTTP(host string) HTTPResult {
	if slices.Contains([]string{"example.com", "example.net", "example.org"}, host) {
		return ProxyDenial(host)
	}
	return HTTPResult{Code: "200", Body: "ok\n"}
}

// probe は egress 自己検証の script の出力を再現する。curl の失敗は exit code 6 (名前解決の失敗) にする。
func (vm *FakeVM) probe(url string) []byte {
	vm.Events = append(vm.Events, "probe "+url)
	host := strings.TrimSuffix(strings.TrimPrefix(url, "https://"), "/")
	result, ok := vm.HTTP[host]
	if !ok {
		result = DefaultHTTP(host)
	}
	if result.Code == "" {
		return []byte("6 000\n" + result.Body)
	}
	return []byte("0 " + result.Code + "\n" + result.Body)
}

// Home は VM の agent user の home。
const Home = "/home/agent"

// Exec はコマンドを VM の中身に当てて、stdout を返す。
func (vm *FakeVM) Exec(command VMCommand) ([]byte, error) {
	vm.ensureMaps()
	args := command.Args
	switch {
	case slices.Equal(args, []string{"printenv", "HOME"}):
		return []byte(Home + "\n"), nil
	case len(args) == 5 && args[0] == "sh" && args[1] == "-c" && args[3] == "sh" && strings.HasPrefix(args[4], "https://"):
		// sh -c '<$1 へ GET を送り、1 行目に curl の exit code と状態コード、続けて body を出す script>' sh <url>
		return vm.probe(args[4]), nil
	case len(args) == 5 && args[0] == "sh" && args[1] == "-c" && args[3] == "sh" && command.Input == nil:
		// sh -c '<$1 があれば yes、無ければ no を出す script>' sh <path>
		if _, ok := vm.Files[args[4]]; ok {
			return []byte("yes\n"), nil
		}
		return []byte("no\n"), nil
	case len(args) == 2 && args[0] == "cat" && vm.FailRead:
		return nil, fmt.Errorf("cat: %s: Permission denied", args[1])
	case len(args) == 2 && args[0] == "cat":
		content, ok := vm.Files[args[1]]
		if !ok {
			return nil, fmt.Errorf("cat: %s: No such file or directory", args[1])
		}
		return []byte(content), nil
	case len(args) >= 5 && args[0] == "sh" && args[1] == "-c" && args[3] == "sh" && command.Input != nil:
		// sh -c '<stdin を $1 へ書く script>' sh <path> [mode]
		if !vm.DropWrites {
			vm.Files[args[4]] = *command.Input
		}
		if len(args) >= 6 {
			vm.Modes[args[4]] = args[5]
		}
		return nil, nil
	case len(args) >= 3 && args[0] == "git" && args[1] == "config" && args[2] == "--global":
		return vm.gitConfig(args[3:])
	case slices.Equal(args, []string{"claude", "plugin", "marketplace", "list", "--json"}):
		type marketplace struct {
			Repo string `json:"repo"`
		}
		list := []marketplace{}
		for _, repo := range vm.Marketplaces {
			list = append(list, marketplace{Repo: repo})
		}
		return json.Marshal(list)
	case len(args) == 5 && slices.Equal(args[:4], []string{"claude", "plugin", "marketplace", "add"}):
		vm.Marketplaces = append(vm.Marketplaces, args[4])
		return nil, nil
	case len(args) == 4 && slices.Equal(args[:3], []string{"claude", "plugin", "install"}):
		if !vm.DropWrites {
			vm.Plugins = append(vm.Plugins, args[3])
		}
		return nil, nil
	case slices.Equal(args, []string{"claude", "plugin", "list", "--json"}):
		type plugin struct {
			ID string `json:"id"`
		}
		list := []plugin{}
		for _, id := range vm.Plugins {
			list = append(list, plugin{ID: id})
		}
		return json.Marshal(list)
	case slices.Equal(args, []string{"pgrep", "-x", "apt-get"}):
		vm.AptPolls++
		if vm.AptPolls <= vm.AptBusyPolls {
			return []byte("123\n"), nil
		}
		return nil, fmt.Errorf("exit status 1")
	case len(args) == 3 && args[0] == "sh" && args[1] == "-c" && strings.HasPrefix(args[2], "pkill -x herdr"):
		vm.Events = append(vm.Events, "stop herdr server")
		return nil, nil
	case len(args) == 3 && args[0] == "bash" && args[1] == "-c":
		vm.ShellRuns = append(vm.ShellRuns, ShellRun{Dir: command.Dir, Command: args[2]})
		vm.Events = append(vm.Events, "shell "+args[2])
		if args[2] == vm.FailShell {
			return []byte("output of the failed command\n"), fmt.Errorf("exit status 1")
		}
		return nil, nil
	case len(args) == 1 && strings.HasPrefix(args[0], "/"):
		if _, ok := vm.Files[args[0]]; !ok {
			return nil, fmt.Errorf("%s: No such file or directory", args[0])
		}
		vm.Executed = append(vm.Executed, args[0])
		vm.Events = append(vm.Events, "exec "+args[0])
		if args[0] == vm.FailExecute {
			return []byte("boot[1] fail exit=1\n"), fmt.Errorf("exit status 1")
		}
		return nil, nil
	}
	return nil, fmt.Errorf("sbxstub: VM で想定外のコマンド %q", args)
}

// bootScript は kit sbxr-boot が起動ごとに実行する boot script の置き場
// (sandbox.BootScriptRelPath と kit の spec.yaml の path の一致は internal/sandbox の test が確かめる)。
const bootScript = Home + "/.config/sbxr/boot.sh"

// Startup は VM の起動で走る kit sbxr-boot の startup を再現する。boot script が実行可能な mode で書かれていれば実行し、
// 無ければ何もしない (kit の `[ -x "$f" ] || exit 0`)。
func (vm *FakeVM) Startup() {
	vm.ensureMaps()
	if _, ok := vm.Files[bootScript]; !ok || vm.Modes[bootScript] != "0755" {
		return
	}
	vm.Executed = append(vm.Executed, bootScript)
	vm.Events = append(vm.Events, "exec "+bootScript)
}

func (vm *FakeVM) gitConfig(args []string) ([]byte, error) {
	switch {
	case len(args) == 2 && args[0] == "--get-all":
		values, ok := vm.GitConfig[args[1]]
		if !ok {
			return nil, fmt.Errorf("exit status 1")
		}
		return []byte(strings.Join(values, "\n") + "\n"), nil
	case len(args) == 3 && args[0] == "--replace-all":
		if !vm.DropWrites {
			vm.GitConfig[args[1]] = []string{args[2]}
		}
		return nil, nil
	case len(args) == 3 && args[0] == "--add":
		if !vm.DropWrites {
			vm.GitConfig[args[1]] = append(vm.GitConfig[args[1]], args[2])
		}
		return nil, nil
	}
	return nil, fmt.Errorf("sbxstub: 想定外の git config %q", args)
}

func (vm *FakeVM) ensureMaps() {
	if vm.Files == nil {
		vm.Files = map[string]string{}
	}
	if vm.Modes == nil {
		vm.Modes = map[string]string{}
	}
	if vm.GitConfig == nil {
		vm.GitConfig = map[string][]string{}
	}
}
