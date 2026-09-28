package sandbox

import (
	"context"
	"fmt"

	"github.com/swat9013/sbxr/internal/runtime"
)

// state は sbxr から見た 1 つの sandbox VM の状態。正本は docs/design/sbxr/statechart.puml で、状態の名前もそれに揃える。
// 管理外と別出所は状態機械の外で、create・stop・destroy のどれも触らずに止まる。
type state int

const (
	// stateAbsent は未作成: VM も、sbxr の管理下の印 (出所) を持つ状態ディレクトリも無い。
	stateAbsent state = iota + 1
	// stateIncompleteStopped は作成途中・停止: 作成が終わった印 (作成時の宣言) が無く、VM は止まっているか無い。
	stateIncompleteStopped
	// stateIncompleteRunning は作成途中・稼働: 作成が終わった印が無く、VM が止まっていない。
	stateIncompleteRunning
	// stateRunning は稼働中: 作成が終わり、VM が止まっていない (使用中かもしれない)。
	stateRunning
	// stateStopped は停止中: 作成が終わり、VM が止まっている。
	stateStopped
	// stateVMGone は VM 消失: 作成が終わったのに、VM が sbxr の外で撤去されている。
	stateVMGone
	// stateUnmanaged は管理外: VM はあるが、sbxr の状態ディレクトリが無い (状態機械の外)。
	stateUnmanaged
	// stateOtherSource は別出所: 同じ名前の状態ディレクトリが別の repo のもの (状態機械の外)。
	stateOtherSource
)

func (s state) String() string {
	switch s {
	case stateAbsent:
		return "未作成"
	case stateIncompleteStopped:
		return "作成途中・停止"
	case stateIncompleteRunning:
		return "作成途中・稼働"
	case stateRunning:
		return "稼働中"
	case stateStopped:
		return "停止中"
	case stateVMGone:
		return "VM 消失"
	case stateUnmanaged:
		return "管理外"
	case stateOtherSource:
		return "別出所"
	}
	return fmt.Sprintf("state(%d)", int(s))
}

// observation は inspect が読んだ sandbox VM の状態と、それを決めた記録。
type observation struct {
	state  state
	status runtime.SandboxStatus
	// record は作成時の記録。作成が終わった VM (稼働中・停止中・VM 消失) だけが持つ。
	record creationRecord
	// recordedSource は状態ディレクトリに記録された出所。別出所のときに見せる。
	recordedSource string
}

// inspect は target の sandbox VM と状態ディレクトリを調べ、状態機械のどの状態にあるかを返す。
func (l Lifecycle) inspect(ctx context.Context, target sandboxTarget) (observation, error) {
	status, err := l.Runtime.SandboxStatus(ctx, target.Name)
	if err != nil {
		return observation{}, err
	}
	stopped := status == runtime.SandboxStopped || status == runtime.SandboxAbsent
	dir := l.Places.stateDirOf(target.Name)
	recorded, found, err := dir.source()
	switch {
	case err != nil:
		return observation{}, err
	case !found && status == runtime.SandboxAbsent:
		return observation{state: stateAbsent, status: status}, nil
	case !found:
		return observation{state: stateUnmanaged, status: status}, nil
	case recorded != target.Source():
		return observation{state: stateOtherSource, status: status, recordedSource: recorded}, nil
	}
	record, completed, err := dir.record()
	switch {
	case err != nil:
		return observation{}, err
	case !completed && stopped:
		return observation{state: stateIncompleteStopped, status: status}, nil
	case !completed:
		return observation{state: stateIncompleteRunning, status: status}, nil
	case status == runtime.SandboxAbsent:
		return observation{state: stateVMGone, status: status, record: record}, nil
	case stopped:
		return observation{state: stateStopped, status: status, record: record}, nil
	}
	return observation{state: stateRunning, status: status, record: record}, nil
}

// outsideMachine は、状態機械の外 (管理外・別出所) の VM に触らない理由を error で返す。状態機械の外の状態でだけ呼ぶ。
func (o observation) outsideMachine(name string) error {
	switch o.state {
	case stateUnmanaged:
		return fmt.Errorf("sandbox VM %s は sbxr の管理外 (sbxr の状態ディレクトリが無い) なので触らない", name)
	case stateOtherSource:
		return fmt.Errorf("sandbox VM %s は別の repo (%s) から作られている", name, o.recordedSource)
	}
	return fmt.Errorf("sandbox VM %s の状態 %s は状態機械の外ではない (sbxr の不具合)", name, o.state)
}
