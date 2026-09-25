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
		Short: "sandbox VM に何が作られるかを表示する (変更しない)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			places, target, err := resolve(deps, args[0])
			if err != nil {
				return err
			}
			prepared, err := sandbox.Prepare(cmd.Context(), deps.clone, places, target, sandbox.KeepRepoEgress)
			if err != nil {
				return err
			}
			return printSummary(cmd, prepared)
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
			ctx := cmd.Context()
			places, target, err := resolve(deps, args[0])
			if err != nil {
				return err
			}
			status, err := deps.runtime.SandboxStatus(ctx, target.Name)
			if err != nil {
				return err
			}
			managed, err := sandbox.Managed(places, target.Name)
			if err != nil {
				return err
			}
			switch {
			case status != runtime.SandboxAbsent && !managed:
				return fmt.Errorf("sandbox VM %s は sbxr の管理外 (sbxr の状態ディレクトリが無い) なので触らない", target.Name)
			case status != runtime.SandboxAbsent:
				printf(cmd, "sandbox VM %s は既にある (作り直すなら sbxr destroy %s → sbxr create %s)\n", target.Name, args[0], args[0])
				return nil
			case managed:
				return fmt.Errorf("sandbox VM %s の前回の作成の残り (%s) がある。sbxr destroy %s で片付けてから作る", target.Name, places.StateDir(target.Name), args[0])
			}
			repoEgress := sandbox.KeepRepoEgress
			if target.FromGitURL() && yes {
				repoEgress = sandbox.DropRepoEgress
			}
			prepared, err := sandbox.Prepare(ctx, deps.clone, places, target, repoEgress)
			if err != nil {
				return err
			}
			if err := printSummary(cmd, prepared); err != nil {
				return err
			}
			if len(prepared.DroppedRepoEgress) > 0 {
				printf(cmd, "git URL を --yes で通したので、repo 宣言の egress (%d 件) を落とした\n", len(prepared.DroppedRepoEgress))
			}
			if err := confirm(deps, yes, fmt.Sprintf("sandbox VM %s を作る? [y/N]: ", target.Name)); err != nil {
				return err
			}
			secretFile, err := deps.secretFilePath()
			if err != nil {
				return err
			}
			values, err := secret.ReadFile(secretFile)
			if err != nil {
				return err
			}
			if err := sandbox.Create(ctx, deps.runtime, places, prepared, values); err != nil {
				return fmt.Errorf("%w\n復旧: sbxr destroy %s で片付けてから sbxr create %s をやり直す", err, args[0], args[0])
			}
			printf(cmd, "sandbox VM %s を作った\n", target.Name)
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
			ctx := cmd.Context()
			places, target, err := resolve(deps, args[0])
			if err != nil {
				return err
			}
			if err := requireManaged(deps, cmd, places, target); err != nil {
				return err
			}
			status, err := deps.runtime.SandboxStatus(ctx, target.Name)
			if err != nil {
				return err
			}
			if status != runtime.SandboxAbsent && status != runtime.SandboxStopped && !force {
				return fmt.Errorf("sandbox VM %s は %s (使用中かを確かめられない)。sbxr stop %s で止めてから撤去するか、--force で撤去する", target.Name, status, args[0])
			}
			printf(cmd, "sandbox VM %s を撤去する。VM 内の commit と変更は失われる\n", target.Name)
			if err := confirm(deps, yes, "撤去する? [y/N]: "); err != nil {
				return err
			}
			warnings, err := sandbox.Destroy(ctx, deps.runtime, places, target)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "警告: %v\n", warning)
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
			if err := requireManaged(deps, cmd, places, target); err != nil {
				return err
			}
			if err := deps.runtime.StopSandbox(cmd.Context(), target.Name); err != nil {
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

// requireManaged は sbxr が作った (状態ディレクトリがある) sandbox VM でなければ止める。
func requireManaged(deps dependencies, cmd *cobra.Command, places sandbox.Places, target sandbox.Target) error {
	managed, err := sandbox.Managed(places, target.Name)
	if err != nil || managed {
		return err
	}
	status, err := deps.runtime.SandboxStatus(cmd.Context(), target.Name)
	if err != nil {
		return err
	}
	if status != runtime.SandboxAbsent {
		return fmt.Errorf("sandbox VM %s は sbxr の管理外 (sbxr の状態ディレクトリが無い) なので触らない", target.Name)
	}
	return fmt.Errorf("sandbox VM %s は無い", target.Name)
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
