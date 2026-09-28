package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

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
			lc, err := lifecycleFor(cmd, deps)
			if err != nil {
				return err
			}
			result, err := lc.Plan(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printf(cmd, "%s", result.Summary)
			if result.Drift != nil { // 既存の VM は、作成時の宣言からの差分も見せる
				printDrift(cmd, *result.Drift)
			}
			return nil
		},
	}
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
			lc, err := lifecycleFor(cmd, deps)
			if err != nil {
				return err
			}
			result, err := lc.Create(cmd.Context(), args[0], promptGate{cmd: cmd, prompter: deps.prompter, yes: yes})
			if result.Drift != nil { // 作成済みの VM は、差分があって error になるときも差分を見せる
				printDrift(cmd, *result.Drift)
			}
			if err != nil {
				return err
			}
			switch result.Outcome {
			case sandbox.Created:
				printf(cmd, "sandbox VM %s を作った\n", result.Name)
			case sandbox.AlreadyCreated:
				printf(cmd, "sandbox VM %s は既にある\n", result.Name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "人間の確認 (確認関門) を省く")
	return cmd
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
			lc, err := lifecycleFor(cmd, deps)
			if err != nil {
				return err
			}
			running := sandbox.RefuseRunning
			if force {
				running = sandbox.RemoveRunning
			}
			result, err := lc.Destroy(cmd.Context(), args[0], promptGate{cmd: cmd, prompter: deps.prompter, yes: yes}, running)
			if err != nil {
				return err
			}
			printf(cmd, "sandbox VM %s を撤去した\n", result.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "撤去の確認を省く")
	cmd.Flags().BoolVar(&force, "force", false, "稼働中 (使用中) の sandbox VM も撤去する")
	return cmd
}

func newStopCmd(deps dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <repo>",
		Short: "sandbox VM を止める (状態は残る)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			lc, err := lifecycleFor(cmd, deps)
			if err != nil {
				return err
			}
			result, err := lc.Stop(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			switch result.Outcome {
			case sandbox.Stopped:
				printf(cmd, "sandbox VM %s を止めた\n", result.Name)
			case sandbox.AlreadyStopped:
				printf(cmd, "sandbox VM %s は止まっている\n", result.Name)
			}
			return nil
		},
	}
}

// lifecycleFor は subcommand の入出力と dependencies で lifecycle を組み立てる。
func lifecycleFor(cmd *cobra.Command, deps dependencies) (sandbox.Lifecycle, error) {
	places, err := deps.places()
	if err != nil {
		return sandbox.Lifecycle{}, err
	}
	userConfig, err := deps.userConfigPath()
	if err != nil {
		return sandbox.Lifecycle{}, err
	}
	return sandbox.Lifecycle{
		Runtime:     deps.runtime,
		Herdr:       deps.herdr,
		Places:      places,
		UserConfig:  userConfig,
		Clone:       deps.clone,
		ReadSecrets: func() (secret.Values, error) { return readSecretFile(deps) },
		Output:      cmd.OutOrStdout(),
		Errors:      cmd.ErrOrStderr(),
	}, nil
}

// promptGate は確認関門を端末で尋ねる。見せる内容は --yes でも表示し、--yes なら尋ねずに承認する。
type promptGate struct {
	cmd      *cobra.Command
	prompter prompter
	yes      bool
}

func (g promptGate) Approve(_ context.Context, proposal sandbox.Proposal) (bool, error) {
	printf(g.cmd, "%s", proposal.Summary)
	if g.yes {
		return true, nil
	}
	// 端末が無ければ prompter が error を返す
	ok, err := g.prompter.Confirm(proposal.Question)
	if err != nil {
		return false, fmt.Errorf("%w (確認を省くなら --yes)", err)
	}
	return ok, nil
}

func (g promptGate) Unattended() bool { return g.yes }

func readSecretFile(deps dependencies) (secret.Values, error) {
	path, err := deps.secretFilePath()
	if err != nil {
		return nil, err
	}
	return secret.ReadFile(path)
}

// printDrift は作成時の宣言と現在の宣言の差分と、比べなかったことの注記を表示する。
func printDrift(cmd *cobra.Command, comparison sandbox.Comparison) {
	differences := comparison.Differences
	if len(differences) == 0 {
		printf(cmd, "drift: 作成時の宣言との差分は無い\n")
	} else {
		printf(cmd, "drift: 作成時の宣言との差分が %d 箇所ある\n", len(differences))
	}
	for _, difference := range differences {
		printf(cmd, "  %s\n    作成時: %s\n    現在:   %s\n", difference.Path, difference.Recorded, difference.Current)
	}
	for _, note := range comparison.NotCompared {
		printf(cmd, "  注記: %s\n", note)
	}
}

// defaultPlaces は XDG の既定に従う置き場。
func defaultPlaces() (sandbox.Places, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return sandbox.Places{}, fmt.Errorf("home ディレクトリを決められない: %w", err)
	}
	return sandbox.Places{
		StateRoot: filepath.Join(xdgDir("XDG_STATE_HOME", home, ".local", "state"), "sbxr", "sandboxes"),
		CacheRoot: filepath.Join(xdgDir("XDG_CACHE_HOME", home, ".cache"), "sbxr", "repos"),
	}, nil
}

func xdgDir(variable, home string, fallback ...string) string {
	if dir := os.Getenv(variable); filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(append([]string{home}, fallback...)...)
}
