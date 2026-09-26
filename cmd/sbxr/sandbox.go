package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/sandbox"
	"github.com/swat9013/sbxr/internal/secret"
)

func newPlanCmd(deps dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "plan <repo>",
		Short: "sandbox VM に何が作られるかを表示する (変更しない)。VM が既にあれば作成時の宣言からの差分も表示する",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			places, target, err := resolve(deps, args[0])
			if err != nil {
				return err
			}
			target, cleanup, err := readableRepo(cmd, deps, target)
			if err != nil {
				return err
			}
			defer cleanup()
			prepared, err := sandbox.Prepare(cmd.Context(), places, target, sandbox.KeepRepoEgress)
			if err != nil {
				return err
			}
			if err := printSummary(cmd, prepared); err != nil {
				return err
			}
			record, found, err := sandbox.ReadRecord(places, target)
			if err != nil {
				return err
			}
			if found { // 既存の VM は、作成時の宣言からの差分も見せる
				differences, _, err := sandbox.CompareWithRecord(cmd.Context(), places, target, record)
				if err != nil {
					return err
				}
				printDrift(cmd, differences)
			}
			printWarnings(cmd, prepared.Warnings)
			return nil
		},
	}
}

// readableRepo は宣言を読むための repo を返す。git URL は一時ディレクトリへ clone する (host に何も残さず、
// 既存の VM の cache clone にも触れない)。cleanup は一時ディレクトリを消す。
func readableRepo(cmd *cobra.Command, deps dependencies, target sandbox.Target) (sandbox.Target, func(), error) {
	if !target.FromGitURL() {
		return target, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "sbxr-read-")
	if err != nil {
		return target, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	target.Repo = filepath.Join(tmp, target.Name)
	if err := sandbox.FreshClone(cmd.Context(), deps.clone, target); err != nil {
		cleanup()
		return target, nil, err
	}
	return target, cleanup, nil
}

func newCreateCmd(deps dependencies) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "create <repo>",
		Short: "宣言を merge し、確認のうえ sandbox VM を作る",
		Long: `宣言を merge し、確認のうえ sandbox VM を作る。
--yes は確認関門を省く。git URL を --yes で通したときは、人間が見ていない repo 宣言の egress を落とす。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			places, target, err := resolve(deps, args[0])
			if err != nil {
				return err
			}
			inspection, err := sandbox.Inspect(ctx, deps.runtime, places, target)
			if err != nil {
				return err
			}
			switch inspection.Situation {
			case sandbox.Ready:
				return reportExisting(cmd, deps, places, target, args[0])
			case sandbox.Incomplete:
				return fmt.Errorf("sandbox VM %s の前回の作成が途中で止まっている。sbxr destroy %s で片付けてから作る", target.Name, args[0])
			case sandbox.Vanished:
				return vanishedError(target.Name, args[0])
			case sandbox.Unmanaged, sandbox.OtherSource:
				return inspection.RequireManaged(target.Name)
			}
			if target.FromGitURL() {
				if err := sandbox.FreshClone(ctx, deps.clone, target); err != nil {
					return err
				}
			}
			created, err := createApproved(cmd, deps, places, target, args[0], yes)
			if !created && target.FromGitURL() {
				// 状態ディレクトリを書く前に止まったので、destroy では見つけられない clone を残さない
				if discardErr := sandbox.DiscardClone(places, target); discardErr != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "警告: %v\n", discardErr)
				}
			}
			if err != nil {
				return err
			}
			printf(cmd, "sandbox VM %s を作った\n", target.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "人間の確認 (確認関門) を省く")
	return cmd
}

// reportExisting は既存の sandbox VM について、作成時の宣言からの drift を表示する。drift があれば非 0 で終える。
// 確認関門にも作成にも進まず、destroy も実行しない。
func reportExisting(cmd *cobra.Command, deps dependencies, places sandbox.Places, target sandbox.Target, input string) error {
	record, found, err := sandbox.ReadRecord(places, target)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("sandbox VM %s の作成時の記録が状態ディレクトリに無い", target.Name)
	}
	target, cleanup, err := readableRepo(cmd, deps, target) // git URL は default branch の HEAD の repo 宣言を読む
	if err != nil {
		return fmt.Errorf("sandbox VM %s は既にある。現在の宣言を読めないので作成時との差分を確かめられない: %w", target.Name, err)
	}
	defer cleanup()
	differences, prepared, err := sandbox.CompareWithRecord(cmd.Context(), places, target, record)
	if err != nil {
		return fmt.Errorf("sandbox VM %s は既にある。現在の宣言を確定できないので作成時との差分を確かめられない: %w", target.Name, err)
	}
	printWarnings(cmd, prepared.Warnings)
	printDrift(cmd, differences)
	if len(differences) > 0 {
		return fmt.Errorf("sandbox VM %s は既にあり、宣言が作成時から変わっている。反映するなら作り直す: sbxr destroy %s → sbxr create %s", target.Name, input, input)
	}
	printf(cmd, "sandbox VM %s は既にある\n", target.Name)
	return nil
}

// printDrift は作成時の宣言と現在の宣言の差分を表示する。
func printDrift(cmd *cobra.Command, differences []sandbox.Difference) {
	if len(differences) == 0 {
		printf(cmd, "drift: 作成時の宣言との差分は無い\n")
		return
	}
	printf(cmd, "drift: 作成時の宣言との差分が %d 箇所ある\n", len(differences))
	for _, difference := range differences {
		printf(cmd, "  %s\n    作成時: %s\n    現在:   %s\n", difference.Path, difference.Recorded, difference.Current)
	}
}

// createApproved は確認関門を通してから作る。created は状態ディレクトリを書くところまで進んだか (失敗しても destroy で片付けられるか)。
func createApproved(cmd *cobra.Command, deps dependencies, places sandbox.Places, target sandbox.Target, input string, yes bool) (created bool, err error) {
	repoEgress := sandbox.KeepRepoEgress
	if target.FromGitURL() && yes {
		repoEgress = sandbox.DropRepoEgress
	}
	prepared, err := sandbox.Prepare(cmd.Context(), places, target, repoEgress)
	if err != nil {
		return false, err
	}
	if err := printSummary(cmd, prepared); err != nil {
		return false, err
	}
	printWarnings(cmd, prepared.Warnings)
	if err := prepared.RequireHerdr(deps.herdr); err != nil { // 確認関門の前に止める
		return false, err
	}
	if len(prepared.DroppedRepoEgress) > 0 {
		printf(cmd, "git URL を --yes で通したので、repo 宣言の egress (%d 件) を落とした\n", len(prepared.DroppedRepoEgress))
	}
	if err := confirm(deps, yes, fmt.Sprintf("sandbox VM %s を作る? [y/N]: ", target.Name)); err != nil {
		return false, err
	}
	values, err := readSecretFile(deps)
	if err != nil {
		return false, err
	}
	if err := sandbox.Create(cmd.Context(), deps.hosts(), places, prepared, values, cmd.OutOrStdout()); err != nil {
		var herdrErr *sandbox.HerdrMachineError
		if errors.As(err, &herdrErr) { // VM は作り終えている
			return true, fmt.Errorf("%w\nsandbox VM %s は作った。復旧: %s", err, target.Name, herdrErr.Recovery)
		}
		var stageErr *sandbox.StageError
		if errors.As(err, &stageErr) { // VM は作れていて、稼働している (destroy は稼働中の VM を拒むので先に止める)
			return true, fmt.Errorf("%w\nsandbox VM %s は調べられるように残した。復旧: sbxr stop %s → sbxr destroy %s → sbxr create %s", err, target.Name, input, input, input)
		}
		return true, fmt.Errorf("%w\n復旧: sbxr destroy %s で片付けてから sbxr create %s をやり直す", err, input, input)
	}
	return true, nil
}

func readSecretFile(deps dependencies) (secret.Values, error) {
	path, err := deps.secretFilePath()
	if err != nil {
		return nil, err
	}
	return secret.ReadFile(path)
}

func newDestroyCmd(deps dependencies) *cobra.Command {
	var yes, force bool
	cmd := &cobra.Command{
		Use:   "destroy <repo>",
		Short: "sandbox VM を撤去する (VM 内の commit と変更は失われる)",
		Long: `sandbox VM を撤去する。VM 内の commit と変更は失われるので、確認を求める (--yes で省く)。
稼働中の VM は使用中かを確かめられないので、sbxr stop で止めてから撤去する。--force は稼働中 (使用中) の VM も撤去する。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			places, target, err := resolve(deps, args[0])
			if err != nil {
				return err
			}
			running := sandbox.RefuseRunning
			if force {
				running = sandbox.RemoveRunning
			}
			// 確認の前に、撤去できない理由があれば伝える (Destroy は撤去の直前にもう一度確かめる)
			inspection, err := sandbox.Inspect(ctx, deps.runtime, places, target)
			if err != nil {
				return err
			}
			if err := inspection.RequireManaged(target.Name); err != nil {
				return err
			}
			if !inspection.NotRunning() && running == sandbox.RefuseRunning {
				return runningError(target.Name, inspection.Status, args[0])
			}
			if err := sandbox.RequireHerdrFor(places, target.Name, deps.herdr); err != nil {
				return err
			}
			printf(cmd, "sandbox VM %s を撤去する。VM 内の commit と変更は失われる\n", target.Name)
			if err := confirm(deps, yes, "撤去する? [y/N]: "); err != nil {
				return err
			}
			warnings, err := sandbox.Destroy(ctx, deps.hosts(), places, target, running)
			var runningErr *sandbox.RunningError
			if errors.As(err, &runningErr) { // 確認の間に起動した
				return runningError(target.Name, runningErr.Status, args[0])
			}
			for _, warning := range warnings {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "警告: %v\n", warning)
			}
			if err != nil {
				return err
			}
			if len(warnings) > 0 {
				return fmt.Errorf("sandbox VM %s は撤去したが、片付けに %d 件失敗した", target.Name, len(warnings))
			}
			printf(cmd, "sandbox VM %s を撤去した\n", target.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "撤去の確認を省く")
	cmd.Flags().BoolVar(&force, "force", false, "稼働中 (使用中) の sandbox VM も撤去する")
	return cmd
}

func runningError(name string, status runtime.SandboxStatus, input string) error {
	return fmt.Errorf("sandbox VM %s は %s (使用中かを確かめられない)。sbxr stop %s で止めてから撤去するか、--force で撤去する", name, status, input)
}

// vanishedError は VM 消失 (状態ディレクトリはあるが VM が sbxr の外で撤去された) の create と stop を止める error。
func vanishedError(name, input string) error {
	return fmt.Errorf("sandbox VM %s は sbxr の外で撤去されている (状態ディレクトリだけが残っている)。sbxr destroy %s で片付ける", name, input)
}

func newStopCmd(deps dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <repo>",
		Short: "sandbox VM を止める (状態は残る)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			places, target, err := resolve(deps, args[0])
			if err != nil {
				return err
			}
			inspection, err := sandbox.Inspect(cmd.Context(), deps.runtime, places, target)
			if err != nil {
				return err
			}
			if err := inspection.RequireManaged(target.Name); err != nil {
				return err
			}
			if inspection.Situation == sandbox.Vanished { // 止める VM が無い。herdr machine にも触れない
				return vanishedError(target.Name, args[0])
			}
			if err := sandbox.Stop(cmd.Context(), deps.hosts(), places, target.Name, cmd.OutOrStdout()); err != nil {
				return err
			}
			printf(cmd, "sandbox VM %s を止めた\n", target.Name)
			return nil
		},
	}
}

func resolve(deps dependencies, input string) (sandbox.Places, sandbox.Target, error) {
	places, err := deps.places()
	if err != nil {
		return sandbox.Places{}, sandbox.Target{}, err
	}
	target, err := sandbox.ResolveTarget(input, places.CacheRoot)
	return places, target, err
}

// confirm は --yes が無ければ人間に確かめる。端末が無ければ prompter が error を返す。
func confirm(deps dependencies, yes bool, prompt string) error {
	if yes {
		return nil
	}
	ok, err := deps.prompter.Confirm(prompt)
	if err != nil {
		return fmt.Errorf("%w (確認を省くなら --yes)", err)
	}
	if !ok {
		return errors.New("中止した")
	}
	return nil
}

func printSummary(cmd *cobra.Command, prepared sandbox.Prepared) error {
	summary, err := prepared.Summary()
	if err != nil {
		return err
	}
	printf(cmd, "%s", summary)
	return nil
}

// defaultPlaces は XDG の既定に従う置き場。
func defaultPlaces() (sandbox.Places, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return sandbox.Places{}, fmt.Errorf("home ディレクトリを決められない: %w", err)
	}
	userConfig, err := defaultUserConfigPath()
	if err != nil {
		return sandbox.Places{}, err
	}
	return sandbox.Places{
		StateRoot:  filepath.Join(xdgDir("XDG_STATE_HOME", home, ".local", "state"), "sbxr", "sandboxes"),
		CacheRoot:  filepath.Join(xdgDir("XDG_CACHE_HOME", home, ".cache"), "sbxr", "repos"),
		UserConfig: userConfig,
	}, nil
}

func xdgDir(variable, home string, fallback ...string) string {
	if dir := os.Getenv(variable); filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(append([]string{home}, fallback...)...)
}

func printWarnings(cmd *cobra.Command, warnings []error) {
	for _, warning := range warnings {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "警告: %v\n", warning)
	}
}
