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
	// Events は bash -c の実行 ("shell <command>") とファイルの実行 ("exec <path>") を起きた順に並べる。
	Events []string
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
