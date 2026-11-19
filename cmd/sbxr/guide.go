package main

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/swat9013/sbxr/internal/guide"
)

func newGuideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "AI coding agent が user 設定と repo 宣言を書くための案内を出す",
		Long: `host の AI coding agent が user 設定と repo 宣言を書くための案内 (書き込みの規則・手順・key の一覧・例) を、
全文そのまま標準出力に出す。案内はこの binary と同じ版の宣言の書き方を持つ。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			// 案内を出し切れなければ (pipe の先が閉じたなど)、agent が途中までの案内で書かないよう非 0 で終える
			_, err := io.WriteString(cmd.OutOrStdout(), guide.Text)
			return err
		},
	}
}
