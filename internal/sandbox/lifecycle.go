package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/secret"
)

// Places は sbxr が host 側に置くもの。
type Places struct {
	// StateRoot は状態ディレクトリの親 (${XDG_STATE_HOME:-~/.local/state}/sbxr/sandboxes。ADR 0006)。
	StateRoot string
	// CacheRoot は git URL の cache clone の親。
	CacheRoot string
}

// Lifecycle は sandbox VM を状態機械 (docs/design/sbxr/statechart.puml) どおりに plan・create・stop・destroy する。
// 入口はどれも、利用者が打った <repo> (path か git URL) を受け取る。状態の調べ・宣言の確定・前提の確認・片付けは
// 入口の内側で行い、失敗には復旧手順を添える (手順には受け取った <repo> をそのまま使う)。
type Lifecycle struct {
	// Runtime は sandbox VM の実行基盤。
	Runtime runtime.Runtime
	// Herdr は host の herdr。herdr 連携を有効にした VM だけが使う。
	Herdr  herdr.Client
	Places Places
	// UserConfig は user 設定の path。
	UserConfig string
	// Clone は git URL を clone する。
	Clone Cloner
	// ReadSecrets は secret ファイルの値を読む。確認関門の後に、配線する secret があるときも無いときも呼ぶ。
	ReadSecrets func() (secret.Values, error)
	// Output は進み具合と案内の出力先。
	Output io.Writer
	// Errors は警告の出力先。
	Errors io.Writer
}

// Gate は確認関門。作る内容や撤去することを見せて、人間の承認を得る。
type Gate interface {
	// Approve は proposal を見せて承認を求める。承認しなければ false。確かめられなければ (端末が無い等) error。
	Approve(ctx context.Context, proposal Proposal) (bool, error)
	// Unattended は人間が見ていないか (--yes)。git URL の repo 宣言の egress は、人間が見ていなければ落とす。
	Unattended() bool
}

// Proposal は確認関門で見せるもの。
type Proposal struct {
	// Summary は承認を求める前に見せる内容 (作る内容、撤去で失われるもの)。
	Summary string
	// Question は承認を求める問い。
	Question string
}

// PlanResult は plan の結果。
type PlanResult struct {
	// Summary は作られる内容。
	Summary string
	// Drift は作成時の宣言との差分。作成済みの VM が無ければ nil。
	Drift *Comparison
}

// CreateResult は create の結果。
type CreateResult struct {
	// Name は sandbox VM の名前。
	Name    string
	Outcome CreateOutcome
	// Drift は作成済みの VM の、作成時の宣言との差分。作ったときは nil。
	Drift *Comparison
}

// CreateOutcome は create が作ったか、作成済みの VM を報告したか。
type CreateOutcome int

const (
	// Created は sandbox VM を作った。
	Created CreateOutcome = iota + 1
	// AlreadyCreated は作成済みの VM があったので作らず、drift を報告した。
	AlreadyCreated
)

// StopResult は stop の結果。
type StopResult struct {
	// Name は sandbox VM の名前。
	Name    string
	Outcome StopOutcome
}

// DestroyResult は destroy の結果。
type DestroyResult struct {
	// Name は撤去した sandbox VM の名前。
	Name string
}

// StopOutcome は stop が VM を止めたか、止まっていたか。
type StopOutcome int

const (
	// Stopped は VM を止めた。
	Stopped StopOutcome = iota + 1
	// AlreadyStopped は VM が止まっていた (VM には触れていない)。
	AlreadyStopped
)

// RunningPolicy は稼働中の VM を撤去するか。
type RunningPolicy int

const (
	// RefuseRunning は稼働中の VM を撤去しない。sbx は使用中かを区別できず、撤去は使用中の VM も消すため (ADR 0006)。
	RefuseRunning RunningPolicy = iota
	// RemoveRunning は稼働中 (使用中かもしれない) の VM も撤去する (--force)。
	RemoveRunning
)

// errDeclined は確認関門で承認されなかったときの error。
var errDeclined = errors.New("中止した")

// Plan は作られる内容を確定して返す。作成済みの VM があれば、作成時の宣言との drift も返す。
// host の管理状態・sandbox VM・global rule のどれも変えない (git URL は一時ディレクトリへ clone して読む)。
func (l Lifecycle) Plan(ctx context.Context, repo string) (PlanResult, error) {
	target, err := resolveTarget(repo, l.Places.CacheRoot)
	if err != nil {
		return PlanResult{}, err
	}
	loaded, err := l.loadCurrentDeclaration(ctx, target)
	if err != nil {
		return PlanResult{}, err
	}
	prepared, err := loaded.prepare(keepRepoEgress)
	if err != nil {
		return PlanResult{}, err
	}
	summary, err := prepared.summary()
	if err != nil {
		return PlanResult{}, err
	}
	result := PlanResult{Summary: summary}
	record, found, err := readRecord(l.Places, target)
	if err != nil || !found {
		return result, err
	}
	comparison, err := loaded.drift(record)
	if err != nil {
		return PlanResult{}, err
	}
	result.Drift = &comparison
	return result, nil
}

// Create は、未作成なら確認関門を通して sandbox VM を作る。作成済みなら作らずに drift を報告し、差分があれば error を返す。
// 作成途中・VM 消失・管理外・別出所なら、何もせずに理由と復旧手順を error で返す。
func (l Lifecycle) Create(ctx context.Context, repo string, gate Gate) (CreateResult, error) {
	target, err := resolveTarget(repo, l.Places.CacheRoot)
	if err != nil {
		return CreateResult{}, err
	}
	observed, err := l.inspect(ctx, target)
	if err != nil {
		return CreateResult{}, err
	}
	name := target.Name
	switch observed.state {
	case stateAbsent:
		return CreateResult{Name: name, Outcome: Created}, l.createFromAbsent(ctx, target, repo, gate)
	case stateRunning, stateStopped:
		return l.reportCreated(ctx, target, observed.record, repo)
	case stateIncompleteStopped:
		return CreateResult{}, fmt.Errorf("sandbox VM %s の前回の作成が途中で止まっている。sbxr destroy %s で片付けてから sbxr create %s で作る", name, repo, repo)
	case stateIncompleteRunning:
		return CreateResult{}, fmt.Errorf("sandbox VM %s の前回の作成が途中で止まっている (VM は稼働している)。sbxr stop %s → sbxr destroy %s で片付けてから sbxr create %s で作る", name, repo, repo, repo)
	case stateVMGone:
		return CreateResult{}, fmt.Errorf("%w。その後 sbxr create %s で作り直す", vanishedError(name, repo), repo)
	case stateUnmanaged, stateOtherSource:
		return CreateResult{}, observed.outsideMachine(name)
	}
	return CreateResult{}, unexpectedState(name, observed.state, "create")
}

// Stop は sandbox VM を止める。herdr 連携を有効にして作った VM は、先に herdr machine を無効にする (ADR 0007)。
// 停止中の VM は、止めずに herdr machine を無効にする (外部停止の後に herdr が VM を起こし直さないように。decision/0010)。
// 作成途中の VM は herdr machine を登録する前なので、herdr には触れない。
func (l Lifecycle) Stop(ctx context.Context, repo string) (StopResult, error) {
	target, err := resolveTarget(repo, l.Places.CacheRoot)
	if err != nil {
		return StopResult{}, err
	}
	observed, err := l.inspect(ctx, target)
	if err != nil {
		return StopResult{}, err
	}
	name := target.Name
	stopped, alreadyStopped := StopResult{Name: name, Outcome: Stopped}, StopResult{Name: name, Outcome: AlreadyStopped}
	switch observed.state {
	case stateRunning:
		return stopped, l.stopRunning(ctx, name)
	case stateIncompleteRunning:
		return stopped, l.Runtime.StopSandbox(ctx, name)
	case stateStopped:
		return alreadyStopped, l.disableHerdrMachine(ctx, name)
	case stateIncompleteStopped:
		return alreadyStopped, nil
	case stateAbsent:
		return StopResult{}, fmt.Errorf("sandbox VM %s は無い", name)
	case stateVMGone: // 止める VM が無い。herdr machine にも触れない
		return StopResult{}, vanishedError(name, repo)
	case stateUnmanaged, stateOtherSource:
		return StopResult{}, observed.outsideMachine(name)
	}
	return StopResult{}, unexpectedState(name, observed.state, "stop")
}

// Destroy は確認関門を通して sandbox VM を撤去し、herdr machine・cache clone・状態ディレクトリを片付ける。
// 稼働中の VM は running に従う。確認の間に起動した VM に備えて、撤去の直前に状態を読み直す。
// VM が無くても実行基盤の撤去を頼む (作成前に置いた sandbox スコープの secret を消すため。ADR 0006)。
// VM を消せなければ、定義を残すために状態ディレクトリを消さずに止める。その後段の失敗は警告して撤去を続け、最後に error を返す。
func (l Lifecycle) Destroy(ctx context.Context, repo string, gate Gate, running RunningPolicy) (DestroyResult, error) {
	target, err := resolveTarget(repo, l.Places.CacheRoot)
	if err != nil {
		return DestroyResult{}, err
	}
	return DestroyResult{Name: target.Name}, l.destroy(ctx, target, repo, gate, running)
}

func (l Lifecycle) destroy(ctx context.Context, target sandboxTarget, repo string, gate Gate, running RunningPolicy) error {
	// 確認の前に、撤去できない理由があれば伝える
	if err := l.requireDestroyable(ctx, target, repo, running); err != nil {
		return err
	}
	if err := l.requireHerdrFor(target.Name); err != nil {
		return err
	}
	approved, err := gate.Approve(ctx, Proposal{
		Summary:  fmt.Sprintf("sandbox VM %s を撤去する。VM 内の commit と変更は失われる\n", target.Name),
		Question: "撤去する? [y/N]: ",
	})
	if err != nil {
		return err
	}
	if !approved {
		return errDeclined
	}
	if err := l.requireDestroyable(ctx, target, repo, running); err != nil { // 確認の間に起動したか
		return err
	}
	return l.remove(ctx, target)
}

// requireDestroyable は target の状態を読み、撤去できなければ理由を error で返す。
func (l Lifecycle) requireDestroyable(ctx context.Context, target sandboxTarget, repo string, running RunningPolicy) error {
	observed, err := l.inspect(ctx, target)
	if err != nil {
		return err
	}
	name := target.Name
	switch observed.state {
	case stateIncompleteStopped, stateStopped, stateVMGone:
		return nil
	case stateIncompleteRunning, stateRunning:
		if running == RemoveRunning {
			return nil
		}
		return fmt.Errorf("sandbox VM %s は %s (使用中かを確かめられない)。sbxr stop %s で止めてから撤去するか、--force で撤去する", name, observed.status, repo)
	case stateAbsent:
		return fmt.Errorf("sandbox VM %s は無い", name)
	case stateUnmanaged, stateOtherSource:
		return observed.outsideMachine(name)
	}
	return unexpectedState(name, observed.state, "destroy")
}

// remove は herdr machine を解除し、VM を sandbox スコープの secret・rule ごと消し、cache clone と状態ディレクトリを片付ける。
func (l Lifecycle) remove(ctx context.Context, target sandboxTarget) error {
	name := target.Name
	dir := l.Places.stateDirOf(name)
	var warnings []error
	// herdr machine の解除は VM を消す前に行う。失敗しても撤去は続ける
	if err := l.removeHerdrMachine(ctx, name); err != nil {
		warnings = append(warnings, err)
	}
	if err := l.Runtime.RemoveEnvironment(ctx, dir.path); err != nil {
		l.warn(warnings...)
		return fmt.Errorf("sandbox VM %s を消せない (状態ディレクトリ %s は残した): %w", name, dir.path, err)
	}
	if target.FromGitURL() {
		if err := discardClone(l.Places, target); err != nil {
			warnings = append(warnings, err)
		}
	}
	if err := dir.remove(); err != nil {
		warnings = append(warnings, err)
	}
	if len(warnings) > 0 {
		l.warn(warnings...)
		return fmt.Errorf("sandbox VM %s は撤去したが、片付けに %d 件失敗した", name, len(warnings))
	}
	return nil
}

// createFromAbsent は未作成の sandbox VM を、確認関門を通して作る。
// 出所を記録する前に止まったら、書きかけの状態ディレクトリと cache clone を消して未作成に戻す (decision/0011)。
func (l Lifecycle) createFromAbsent(ctx context.Context, target sandboxTarget, repo string, gate Gate) (err error) {
	recorded := false
	defer func() {
		if err != nil && !recorded {
			l.discardUnrecorded(target)
		}
	}()
	if target.FromGitURL() {
		if err := freshClone(ctx, l.Clone, target); err != nil {
			return err
		}
	}
	loaded, err := l.loadDeclaration(ctx, target, target.Repo)
	if err != nil {
		return err
	}
	repoEgress := keepRepoEgress
	if target.FromGitURL() && gate.Unattended() {
		repoEgress = dropRepoEgress
	}
	prepared, err := loaded.prepare(repoEgress)
	if err != nil {
		return err
	}
	if prepared.Declaration.Herdr != nil { // 確認関門の前に止める
		if err := requireHerdrOnHost(l.Herdr); err != nil {
			return err
		}
	}
	summary, err := prepared.summary()
	if err != nil {
		return err
	}
	if len(prepared.DroppedRepoEgress) > 0 {
		summary += fmt.Sprintf("git URL を --yes で通したので、repo 宣言の egress (%d 件) を落とした\n", len(prepared.DroppedRepoEgress))
	}
	approved, err := gate.Approve(ctx, Proposal{Summary: summary, Question: fmt.Sprintf("sandbox VM %s を作る? [y/N]: ", target.Name)})
	if err != nil {
		return err
	}
	if !approved {
		return errDeclined
	}
	values, err := l.ReadSecrets()
	if err != nil {
		return err
	}
	if err := prepared.Wiring.RequireValues(values); err != nil { // 状態ディレクトリを書く前に止める (UC2 5a)
		return err
	}
	return l.build(ctx, prepared, values, repo, &recorded)
}

// build は作る内容を組み立てて実行基盤に定義と作成を頼み (secret と rule をどの順で置くかは実行基盤が守る。decision/0009)、
// VM の中を宣言どおりにし (materialize → read-back → init → boot)、VM 内から egress 自己検証を行う。
// 状態ディレクトリは定義 → 作成の最初の記録 → 出所の順に書き、出所を書けたら recorded を立てる。
// 出所を書いた後の失敗は、状態ディレクトリと VM を残す (destroy がそれを使って片付ける)。
// 作成が終わった印 (作成時の宣言) は最後に書き、herdr machine はその後に登録する。
func (l Lifecycle) build(ctx context.Context, prepared preparation, values secret.Values, repo string, recorded *bool) error {
	rt := l.Runtime
	name := prepared.Target.Name
	dir := l.Places.stateDirOf(name)
	spec, err := sandboxSpec(prepared, values)
	if err != nil {
		return err
	}
	if err := dir.ensure(); err != nil {
		return err
	}
	if err := rt.DefineSandbox(dir.path, spec); err != nil {
		return err
	}
	herdrEnabled := prepared.Declaration.Herdr != nil
	if err := dir.writeCreation(prepared.Target.Source(), creation{Herdr: &herdrEnabled}); err != nil {
		return err
	}
	*recorded = true
	// VM は稼働したまま残る (destroy は稼働中の VM を拒むので、先に止める)
	leftRunning := func(err error) error {
		return fmt.Errorf("%w\nsandbox VM %s は調べられるように残した。復旧: sbxr stop %s → sbxr destroy %s → sbxr create %s", err, name, repo, repo, repo)
	}
	if err := rt.CreateSandbox(ctx, dir.path, spec); err != nil {
		var created *runtime.CreatedError
		if errors.As(err, &created) {
			return leftRunning(createdStageError(created))
		}
		return fmt.Errorf("%w\n復旧: sbxr destroy %s で片付けてから sbxr create %s をやり直す", err, repo, repo)
	}
	if err := setUpInside(ctx, rt, prepared, l.Output); err != nil {
		return leftRunning(err)
	}
	if err := dir.writeRecord(creationRecord{Declaration: prepared.Declaration, RepoEgress: prepared.RepoEgress}); err != nil {
		return leftRunning(err)
	}
	if !herdrEnabled {
		return nil
	}
	if err := l.registry().Register(ctx, name, l.Output); err != nil {
		var registration *herdr.RegistrationError
		if errors.As(err, &registration) { // VM は作り終えている
			return fmt.Errorf("%w\nsandbox VM %s は作った。復旧: %s", err, name, registration.Recovery)
		}
		return err
	}
	return nil
}

// discardUnrecorded は出所を記録する前に止まった create の書きかけ (状態ディレクトリと cache clone) を消す。
// 出所の無い状態ディレクトリは、sbx の側に何も置いていない (decision/0011)。消せなければ警告する。
func (l Lifecycle) discardUnrecorded(target sandboxTarget) {
	var warnings []error
	if err := l.Places.stateDirOf(target.Name).remove(); err != nil {
		warnings = append(warnings, err)
	}
	if target.FromGitURL() {
		if err := discardClone(l.Places, target); err != nil {
			warnings = append(warnings, err)
		}
	}
	l.warn(warnings...)
}

// reportCreated は作成済みの sandbox VM について、作成時の宣言からの drift を返す。drift があれば error も返す。
// 作成にも destroy にも進まない。
func (l Lifecycle) reportCreated(ctx context.Context, target sandboxTarget, record creationRecord, repo string) (CreateResult, error) {
	// git URL は default branch の HEAD の repo 宣言を読む (VM の cache clone には触れない)
	loaded, err := l.loadCurrentDeclaration(ctx, target)
	if err != nil {
		return CreateResult{}, fmt.Errorf("sandbox VM %s は既にある。現在の宣言を確定できないので作成時との差分を確かめられない: %w", target.Name, err)
	}
	comparison, err := loaded.drift(record)
	if err != nil {
		return CreateResult{}, fmt.Errorf("sandbox VM %s は既にある。現在の宣言を確定できないので作成時との差分を確かめられない: %w", target.Name, err)
	}
	result := CreateResult{Name: target.Name, Outcome: AlreadyCreated, Drift: &comparison}
	if len(comparison.Differences) > 0 {
		return result, fmt.Errorf("sandbox VM %s は既にあり、宣言が作成時から変わっている。反映するなら作り直す: sbxr destroy %s → sbxr create %s", target.Name, repo, repo)
	}
	return result, nil
}

// loadDeclaration は repoDir の宣言を読み、警告を出す。
func (l Lifecycle) loadDeclaration(ctx context.Context, target sandboxTarget, repoDir string) (loadedDeclaration, error) {
	loaded, err := loadDeclaration(ctx, l.UserConfig, target, repoDir)
	if err != nil {
		return loadedDeclaration{}, err
	}
	l.warn(loaded.warnings...)
	return loaded, nil
}

// loadCurrentDeclaration は target の現在の宣言を読む。git URL は一時ディレクトリへ clone して読み、読み終えたら消す
// (host に何も残さず、作成済みの VM の cache clone にも触れない)。
func (l Lifecycle) loadCurrentDeclaration(ctx context.Context, target sandboxTarget) (loadedDeclaration, error) {
	if !target.FromGitURL() {
		return l.loadDeclaration(ctx, target, target.Repo)
	}
	tmp, err := os.MkdirTemp("", "sbxr-read-")
	if err != nil {
		return loadedDeclaration{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	readable := target
	readable.Repo = filepath.Join(tmp, target.Name)
	if err := freshClone(ctx, l.Clone, readable); err != nil {
		return loadedDeclaration{}, err
	}
	return l.loadDeclaration(ctx, target, readable.Repo)
}

// drift は現在の宣言を作成時と同じ repo の egress の扱いで確定し、作成時の宣言と比べる。
func (d loadedDeclaration) drift(record creationRecord) (Comparison, error) {
	current, err := d.prepare(record.RepoEgress)
	if err != nil {
		return Comparison{}, err
	}
	return record.Drift(current.Declaration)
}

// sandboxSpec は確定した宣言と secret の値から、実行基盤に渡す作る内容を組み立てる。
// secret の値を含むので、確認関門と plan が見せる preparation には載せず、作る直前に組み立てる。
func sandboxSpec(prepared preparation, values secret.Values) (runtime.SandboxSpec, error) {
	secrets, err := prepared.Wiring.SandboxSecrets(values)
	if err != nil {
		return runtime.SandboxSpec{}, err
	}
	spec := runtime.SandboxSpec{
		Name:        prepared.Target.Name,
		Repo:        prepared.Target.Repo,
		Env:         prepared.VMEnv,
		Secrets:     secrets,
		EgressRules: prepared.Declaration.SandboxEgress,
		// boot を宣言していなくても再生を頼む (script を置かなければ何も走らない)。再生の仕組みの無い VM を
		// 実 sbx で確かめていないので、作る VM の形を変えない
		ReplayBoot: true,
	}
	if pin := prepared.Declaration.Herdr; pin != nil {
		spec.Herdr = &runtime.HerdrInstall{Version: pin.Version}
	}
	return spec, nil
}

// createdStageError は VM を作れた後の段の失敗を、表示する段の error にする。
// 知らない段でも *StageError にする (VM が残っていることを落とさない)。
func createdStageError(created *runtime.CreatedError) error {
	switch created.Step {
	case runtime.CreatedStepSandboxEgress:
		return stageError(StageSandboxEgress, created.Err)
	case runtime.CreatedStepHerdrStartup:
		return stageError(StageHerdr, created.Err)
	}
	return stageError(Stage(fmt.Sprintf("sandbox VM を作った後の段 %d", created.Step)), created.Err)
}

// discardClone は git URL の Target の cache clone を消す。cacheRoot の直下にあるものだけを消す (利用者の repo を消さないため)。
func discardClone(places Places, target sandboxTarget) error {
	if !target.FromGitURL() || filepath.Dir(filepath.Clean(target.Repo)) != filepath.Clean(places.CacheRoot) {
		return fmt.Errorf("cache clone でない %s は消さなかった", target.Repo)
	}
	if err := os.RemoveAll(target.Repo); err != nil {
		return fmt.Errorf("cache clone %s を消せない: %w", target.Repo, err)
	}
	return nil
}

// vanishedError は VM 消失 (状態ディレクトリはあるが VM が sbxr の外で撤去された) の create と stop を止める error。
func vanishedError(name, repo string) error {
	return fmt.Errorf("sandbox VM %s は sbxr の外で撤去されている (状態ディレクトリだけが残っている)。sbxr destroy %s で片付ける", name, repo)
}

// unexpectedState は、入口が扱いを決めていない状態に着いたときの error。状態を足したのに入口を直していない不具合で、
// 黙って作成や撤去へ進まないように止める。
func unexpectedState(name string, s state, event string) error {
	return fmt.Errorf("sandbox VM %s の状態 %s での %s を sbxr が扱えない (sbxr の不具合)", name, s, event)
}

func (l Lifecycle) warn(warnings ...error) {
	for _, warning := range warnings {
		logf(l.Errors, "警告: %v\n", warning)
	}
}
