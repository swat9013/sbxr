package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/egress"
)

func newPolicyCmd(deps dependencies) *cobra.Command {
	policy := &cobra.Command{
		Use:   "policy",
		Short: "全 sandbox VM に効く egress の global rule を扱う",
	}
	policy.AddCommand(newPolicySyncCmd(deps))
	return policy
}

func newPolicySyncCmd(deps dependencies) *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "default と user 設定の egress 宣言へ global rule を収束させる",
		Long: `default と user 設定の egress 宣言へ global rule を収束させる。
対象は全 sandbox VM に効く global rule のうち実行基盤が編集を許すもので、宣言に無いものは手で足した rule も消す。
sandbox スコープ rule と、実行基盤自身が管理する rule には触れない。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true // ここから先の失敗は使い方の誤りではない
			userConfigPath, err := deps.userConfigPath()
			if err != nil {
				return err
			}
			desired, err := desiredGlobalResources(userConfigPath)
			if err != nil {
				return err
			}
			if checkOnly {
				return checkGlobalRules(cmd, deps, desired)
			}
			return convergeGlobalRules(cmd, deps, desired)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "差分を表示するだけで変更しない (差分があれば非 0 で終わる)")
	return cmd
}

func desiredGlobalResources(userConfigPath string) ([]string, error) {
	declared, err := config.LoadGlobalEgress(userConfigPath)
	if err != nil {
		return nil, err
	}
	groups, err := egress.ParseGroups(declared)
	if err != nil {
		return nil, err
	}
	return egress.DesiredResources(groups), nil
}

func checkGlobalRules(cmd *cobra.Command, deps dependencies, desired []string) error {
	changes, err := egress.Diff(cmd.Context(), deps.runtime, desired)
	if err != nil {
		return err
	}
	if changes.Empty() {
		printf(cmd, "global rule は宣言と一致している (宛先 %d)\n", len(desired))
		return nil
	}
	printChanges(cmd, changes)
	return errors.New("global rule が宣言と一致しない (sbxr policy sync で収束させる)")
}

func convergeGlobalRules(cmd *cobra.Command, deps dependencies, desired []string) error {
	changes, err := egress.Converge(cmd.Context(), deps.runtime, desired)
	printChanges(cmd, changes) // 途中で失敗しても、そこまでに行った変更は見せる
	if err != nil {
		return err
	}
	printf(cmd, "global rule を宣言へ収束させた (消した rule %d / 足した宛先 %d / 宛先 %d)\n", len(changes.Remove), len(changes.Add), len(desired))
	return nil
}

func printChanges(cmd *cobra.Command, changes egress.Changes) {
	for _, rule := range changes.Remove {
		printf(cmd, "- rm %s (%s: %s)\n", rule.ID, rule.Decision, strings.Join(rule.Resources, ", "))
	}
	for _, resource := range changes.Add {
		printf(cmd, "+ allow %s\n", resource)
	}
}

// printf は結果を stdout へ書く。書き込みの失敗 (閉じた pipe など) は結果を伝えられないので無視する。
func printf(cmd *cobra.Command, format string, args ...any) {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), format, args...)
}
