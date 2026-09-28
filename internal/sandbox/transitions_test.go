package sandbox

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
)

// 状態機械 (docs/design/sbxr/statechart.puml) の遷移を 1 行ずつ並べた表。状態 × イベントの組み合わせは、遷移か
// 「起きない」(uncovered) のどちらかで全部を埋める。行が statechart.puml と 1 対 1 に対応することは TestTheTableHasExactlyTheLinesOfTheStatechart が確かめる。

// event は状態機械のイベント。sbxr が起こす 3 つと、sbxr の外で起きる 3 つ。
type event string

const (
	eventCreate        event = "create"
	eventStop          event = "stop"
	eventDestroy       event = "destroy"
	eventStartOutside  event = "外部起動"
	eventStopOutside   event = "外部停止"
	eventRemoveOutside event = "外部撤去"
)

var (
	machineStates = []state{stateAbsent, stateIncompleteStopped, stateIncompleteRunning, stateRunning, stateStopped, stateVMGone}
	machineEvents = []event{eventCreate, eventStop, eventDestroy, eventStartOutside, eventStopOutside, eventRemoveOutside}
)

// transition は表の 1 行。
type transition struct {
	from  state
	event event
	// guard は同じイベントの結果の分岐 (statechart の [guard])。arrange がその条件を作る。
	guard   string
	arrange func(w *world)
	// act はイベントを起こす。nil なら event の既定の起こし方。
	act func(w *world) error
	to  state
	// refused はイベントが拒否か失敗を error で返すか。
	refused bool
	// then は遷移に伴う action (statechart の / action) を確かめる。nil なら遷移先だけを見る。
	then func(t *testing.T, w *world)
	// unimplemented は、この行の遷移がまだ無く、それを埋める issue。空なら実装済み (test を走らせる)。
	unimplemented string
	// pending は、遷移は実装済みで action の一部が未実装のとき、その部分と埋める issue。
	pending string
}

func failDefine(w *world)         { w.failDefine = true }
func failEnvCreate(w *world)      { w.failEnvCreate = true }
func failAfterCreatedVM(w *world) { w.failAfterCreated = runtime.CreatedStepSandboxEgress }

func destroyForcing(w *world) error { return w.destroy(w.repo(), RemoveRunning) }

// --- action の検査 ---

func registersTheHerdrMachine(t *testing.T, w *world) {
	if len(w.herdr.Machines) != 1 || !w.herdr.Machines[0].Enabled {
		t.Errorf("herdr machines = %v, want the machine registered", w.herdr.Machines)
	}
}

// leavesTheHerdrMachineEnabled は herdr machine が有効のまま残ることを確かめる (herdr が VM を起こし直しうる)。
func leavesTheHerdrMachineEnabled(t *testing.T, w *world) {
	if len(w.herdr.Machines) != 1 || !w.herdr.Machines[0].Enabled {
		t.Errorf("herdr machines = %v, want the machine left enabled", w.herdr.Machines)
	}
}

func leavesNoStateDir(t *testing.T, w *world) {
	if exists(w.places.stateDirPath("app")) {
		t.Errorf("state dir was left")
	}
}

func keepsTheStateDir(t *testing.T, w *world) {
	if !exists(w.places.stateDirPath("app")) {
		t.Errorf("state dir was removed; destroy needs it to clean up")
	}
}

func touchesNoVM(t *testing.T, w *world) {
	if len(w.stops) != 0 {
		t.Errorf("sbx stops = %v, want the VM untouched", w.stops)
	}
}

func stopsTheVMOnce(t *testing.T, w *world) {
	if len(w.stops) != 1 {
		t.Errorf("sbx stops = %v, want the VM stopped once", w.stops)
	}
}

func disablesTheHerdrMachine(t *testing.T, w *world) {
	if len(w.herdr.Machines) != 1 || w.herdr.Machines[0].Enabled {
		t.Errorf("herdr machines = %v, want the machine disabled", w.herdr.Machines)
	}
}

func disablesTheHerdrMachineAndStops(t *testing.T, w *world) {
	disablesTheHerdrMachine(t, w)
	if len(w.stops) != 1 {
		t.Errorf("sbx stops = %v, want the VM stopped once", w.stops)
	}
}

func disablesTheHerdrMachineWithoutTouchingTheVM(t *testing.T, w *world) {
	disablesTheHerdrMachine(t, w)
	touchesNoVM(t, w)
}

// removesEverything は VM と sandbox スコープの secret・rule (実行基盤の entry ごと)、herdr machine、状態ディレクトリが無いことを確かめる。
func removesEverything(t *testing.T, w *world) {
	if _, ok := w.vms.Sandboxes["app"]; ok || len(w.herdr.Machines) != 0 || exists(w.places.stateDirPath("app")) {
		t.Errorf("sandbox = %+v, herdr machines = %v, want the sandbox, its machine and the state dir removed", w.vms.Sandboxes["app"], w.herdr.Machines)
	}
}

func createsNothingNew(t *testing.T, w *world) {
	if w.definitionsAtArrival != len(w.vms.Definitions) || len(w.stops) != 0 {
		t.Errorf("definitions = %d → %d, stops = %v, want nothing created or stopped", w.definitionsAtArrival, len(w.vms.Definitions), w.stops)
	}
}

var transitions = []transition{
	// --- create ---
	{from: stateAbsent, event: eventCreate, guard: "成功", to: stateRunning, then: registersTheHerdrMachine,
		pending: "template の用意は #13"},
	{from: stateAbsent, event: eventCreate, guard: "出所の記録前に失敗", arrange: failDefine, to: stateAbsent, refused: true, then: leavesNoStateDir,
		pending: "build 用 VM の片付けは #13"},
	{from: stateAbsent, event: eventCreate, guard: "VM 作成前に失敗", arrange: failEnvCreate, to: stateIncompleteStopped, refused: true, then: keepsTheStateDir},
	{from: stateAbsent, event: eventCreate, guard: "VM 作成後に失敗", arrange: failAfterCreatedVM, to: stateIncompleteRunning, refused: true, then: keepsTheStateDir},
	{from: stateIncompleteStopped, event: eventCreate, to: stateIncompleteStopped, refused: true, then: createsNothingNew},
	{from: stateIncompleteRunning, event: eventCreate, to: stateIncompleteRunning, refused: true, then: createsNothingNew},
	{from: stateRunning, event: eventCreate, to: stateRunning, then: createsNothingNew},
	{from: stateStopped, event: eventCreate, to: stateStopped, then: createsNothingNew},
	{from: stateVMGone, event: eventCreate, to: stateVMGone, refused: true, then: createsNothingNew},

	// --- stop ---
	{from: stateAbsent, event: eventStop, to: stateAbsent, refused: true, then: touchesNoVM},
	{from: stateIncompleteStopped, event: eventStop, to: stateIncompleteStopped, then: touchesNoVM},
	{from: stateIncompleteRunning, event: eventStop, to: stateIncompleteStopped, then: stopsTheVMOnce},
	{from: stateRunning, event: eventStop, to: stateStopped, then: disablesTheHerdrMachineAndStops},
	{from: stateStopped, event: eventStop, to: stateStopped, then: disablesTheHerdrMachineWithoutTouchingTheVM},
	{from: stateVMGone, event: eventStop, to: stateVMGone, refused: true, then: touchesNoVM},

	// --- destroy ---
	{from: stateAbsent, event: eventDestroy, to: stateAbsent, refused: true},
	{from: stateIncompleteStopped, event: eventDestroy, to: stateAbsent, then: removesEverything},
	{from: stateIncompleteRunning, event: eventDestroy, guard: "--force 無し", to: stateIncompleteRunning, refused: true, then: keepsTheStateDir},
	{from: stateIncompleteRunning, event: eventDestroy, guard: "--force", act: destroyForcing, to: stateAbsent, then: removesEverything},
	{from: stateRunning, event: eventDestroy, guard: "--force 無し", to: stateRunning, refused: true, then: keepsTheStateDir},
	{from: stateRunning, event: eventDestroy, guard: "--force", act: destroyForcing, to: stateAbsent, then: removesEverything},
	{from: stateStopped, event: eventDestroy, guard: "未回収あり ∨ 検査に失敗", to: stateStopped, refused: true, unimplemented: "#9"},
	{from: stateStopped, event: eventDestroy, guard: "未回収なし ∨ 検査対象外", to: stateAbsent, then: removesEverything,
		pending: "一時起動して未回収を検査し、止め直すのは #9 (今は検査せずに撤去する)"},
	{from: stateStopped, event: eventDestroy, guard: "--force", act: destroyForcing, to: stateAbsent, then: removesEverything},
	{from: stateVMGone, event: eventDestroy, to: stateAbsent, then: removesEverything},

	// --- sbxr の外で起きること ---
	{from: stateIncompleteStopped, event: eventStartOutside, guard: "VM がある", to: stateIncompleteRunning},
	{from: stateStopped, event: eventStartOutside, to: stateRunning},
	{from: stateIncompleteRunning, event: eventStopOutside, to: stateIncompleteStopped},
	{from: stateRunning, event: eventStopOutside, to: stateStopped, then: leavesTheHerdrMachineEnabled},
	{from: stateIncompleteStopped, event: eventRemoveOutside, guard: "VM がある", to: stateIncompleteStopped},
	{from: stateIncompleteRunning, event: eventRemoveOutside, to: stateIncompleteStopped},
	{from: stateRunning, event: eventRemoveOutside, to: stateVMGone},
	{from: stateStopped, event: eventRemoveOutside, to: stateVMGone},
}

// uncovered は起きない組み合わせと、起きない理由。外部のイベントは、起こす前提 (VM がある・止まっている・動いている) が
// 無いので起こせないことを確かめる。
var uncovered = []struct {
	from   state
	event  event
	reason string
}{
	{stateAbsent, eventStartOutside, "VM が無い"},
	{stateAbsent, eventStopOutside, "VM が無い"},
	{stateAbsent, eventRemoveOutside, "VM が無い"},
	{stateIncompleteStopped, eventStopOutside, "止まっているか VM が無いので、止める対象が無い"},
	{stateIncompleteRunning, eventStartOutside, "既に稼働している"},
	{stateRunning, eventStartOutside, "既に稼働している"},
	{stateStopped, eventStopOutside, "既に止まっている"},
	{stateVMGone, eventStartOutside, "VM が無い"},
	{stateVMGone, eventStopOutside, "VM が無い"},
	{stateVMGone, eventRemoveOutside, "VM が無い"},
}

// statechartIDs は statechart.puml の状態 id。
var statechartIDs = map[string]state{
	"Absent": stateAbsent, "IncompleteStopped": stateIncompleteStopped, "IncompleteRunning": stateIncompleteRunning,
	"Running": stateRunning, "Stopped": stateStopped, "VMGone": stateVMGone,
}

var (
	statechartTransition = regexp.MustCompile(`^(\w+) --> (\w+) : (\S+)(?: \[([^\]]+)\])?`)
	statechartUncovered  = regexp.MustCompile(`^' uncovered: (\w+) x (\S+) — `)
)

// 表を statechart.puml と突き合わせる。puml の遷移行 (状態 × イベント × guard → 状態) と uncovered 宣言が、表の行と 1 対 1 に対応する。
func TestTheTableHasExactlyTheLinesOfTheStatechart(t *testing.T) {
	data, err := os.ReadFile("../../docs/design/sbxr/statechart.puml")
	if err != nil {
		t.Fatal(err)
	}
	var fromChart []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if m := statechartTransition.FindStringSubmatch(line); m != nil {
			fromChart = append(fromChart, fmt.Sprintf("%s × %s [%s] → %s", statechartIDs[m[1]], m[3], m[4], statechartIDs[m[2]]))
		} else if m := statechartUncovered.FindStringSubmatch(line); m != nil {
			fromChart = append(fromChart, fmt.Sprintf("%s × %s 起きない", statechartIDs[m[1]], m[2]))
		}
	}
	var fromTable []string
	for _, tr := range transitions {
		fromTable = append(fromTable, fmt.Sprintf("%s × %s [%s] → %s", tr.from, tr.event, tr.guard, tr.to))
	}
	for _, u := range uncovered {
		fromTable = append(fromTable, fmt.Sprintf("%s × %s 起きない", u.from, u.event))
	}
	slices.Sort(fromChart)
	slices.Sort(fromTable)

	if !slices.Equal(fromChart, fromTable) {
		t.Errorf("table and statechart.puml differ\nstatechart: %q\ntable:      %q", fromChart, fromTable)
	}
}

func TestEveryStateAndEventIsATransitionOrUncovered(t *testing.T) {
	covered := map[string]bool{}
	for _, tr := range transitions {
		covered[fmt.Sprint(tr.from, tr.event)] = true
	}
	for _, u := range uncovered {
		key := fmt.Sprint(u.from, u.event)
		if covered[key] {
			t.Errorf("%s × %s is both a transition and uncovered", u.from, u.event)
		}
		covered[key] = true
	}
	for _, s := range machineStates {
		for _, e := range machineEvents {
			if !covered[fmt.Sprint(s, e)] {
				t.Errorf("%s × %s is neither a transition nor uncovered", s, e)
			}
		}
	}
}

func TestTransitions(t *testing.T) {
	for _, tr := range transitions {
		name := fmt.Sprintf("%s × %s", tr.from, tr.event)
		if tr.guard != "" {
			name += " [" + tr.guard + "]"
		}
		t.Run(name, func(t *testing.T) {
			if tr.unimplemented != "" {
				t.Skipf("未実装: %s が埋める", tr.unimplemented)
			}
			if tr.pending != "" {
				t.Logf("action の一部が未実装: %s", tr.pending)
			}
			w := newWorld(t, herdrUserConfig)
			w.arrive(tr.from)
			if tr.arrange != nil {
				tr.arrange(w)
			}
			act := tr.act
			if act == nil {
				act = w.defaultAct(tr.event)
			}

			err := act(w)

			if refused := err != nil; refused != tr.refused {
				t.Errorf("error = %v, want refused = %v", err, tr.refused)
			}
			if got := w.stateOf(w.repo()); got != tr.to {
				t.Errorf("state = %s, want %s", got, tr.to)
			}
			if tr.then != nil {
				tr.then(t, w)
			}
		})
	}
}

func TestUncoveredCombinationsCannotHappen(t *testing.T) {
	for _, u := range uncovered {
		t.Run(fmt.Sprintf("%s × %s", u.from, u.event), func(t *testing.T) {
			w := newWorld(t, testUserConfig)
			w.arrive(u.from)

			err := w.defaultAct(u.event)(w)

			if !errors.Is(err, errCannotHappen) {
				t.Errorf("%s in %s = %v, want it not to happen (%s)", u.event, u.from, err, u.reason)
			}
		})
	}
}

// repo は表の sandbox VM app の repo。world ごとに 1 つ作る。
func (w *world) repo() string {
	if w.repoPath == "" {
		w.repoPath = localRepo(w.t, "app", "")
	}
	return w.repoPath
}

// arrive は sandbox VM app を状態 s にする。作成途中・停止は、VM がある形で作る (外部起動・外部撤去の [VM がある] のため)。
func (w *world) arrive(s state) {
	w.t.Helper()
	repo := w.repo()
	switch s {
	case stateAbsent:
	case stateRunning:
		w.mustCreate(repo)
	case stateStopped:
		w.mustCreate(repo)
		w.mustStop(repo)
	case stateIncompleteRunning:
		w.failAfterCreated = runtime.CreatedStepSandboxEgress
		if _, err := w.create(repo, unattended()); err == nil {
			w.t.Fatal("Create() error = nil, want the creation stopped after the VM was created")
		}
		w.failAfterCreated = 0
	case stateIncompleteStopped:
		w.arrive(stateIncompleteRunning)
		w.mustStop(repo)
	case stateVMGone:
		w.mustCreate(repo)
		_ = w.removeOutside()
	case stateUnmanaged, stateOtherSource: // 状態機械の外へは運ばない (下の確認で止まる)
	}
	if got := w.stateOf(repo); got != s {
		w.t.Fatalf("arrived at %s, want %s", got, s)
	}
	w.stops = nil
	w.definitionsAtArrival = len(w.vms.Definitions)
}

// errCannotHappen は、前提が無いので起こせない外部のイベント。
var errCannotHappen = errors.New("このイベントは起きない")

func (w *world) defaultAct(e event) func(w *world) error {
	switch e {
	case eventCreate:
		return func(w *world) error { _, err := w.create(w.repo(), unattended()); return err }
	case eventStop:
		return func(w *world) error { _, err := w.stop(w.repo()); return err }
	case eventDestroy:
		return func(w *world) error { return w.destroy(w.repo(), RefuseRunning) }
	case eventStartOutside:
		return func(w *world) error { return w.startOutside() }
	case eventStopOutside:
		return func(w *world) error { return w.stopOutside() }
	case eventRemoveOutside:
		return func(w *world) error { return w.removeOutside() }
	}
	w.t.Fatalf("unknown event %s", e)
	return nil
}

// startOutside は sbx exec や herdr の再接続で、止まっている VM を起こす。
func (w *world) startOutside() error {
	vm, ok := w.vms.Sandboxes["app"]
	if !ok || vm.Status != runtime.SandboxStopped {
		return errCannotHappen
	}
	vm.Status = runtime.SandboxRunning
	return nil
}

// stopOutside は sbx stop で、動いている VM を止める。herdr machine には触れない。
func (w *world) stopOutside() error {
	vm, ok := w.vms.Sandboxes["app"]
	if !ok || vm.Status != runtime.SandboxRunning {
		return errCannotHappen
	}
	vm.Status = runtime.SandboxStopped
	return nil
}

// removeOutside は sbx rm で VM だけを消す。状態ディレクトリと sandbox スコープの secret は残る。
func (w *world) removeOutside() error {
	vm, ok := w.vms.Sandboxes["app"]
	if !ok || vm.Status == runtime.SandboxAbsent {
		return errCannotHappen
	}
	vm.Status = runtime.SandboxAbsent
	return nil
}
