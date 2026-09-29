package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/swat9013/sbxr/internal/doctor"
)

func newDoctorCmd(deps dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [<repo>]",
		Short: "user 設定・repo 宣言と host の前提を全件検査する (変更しない)",
		Long: `user 設定・repo 宣言と host の前提 (sbx・secret ファイル・herdr・global rule) を全件検査する。
項目ごとに ok / fail / skip を表示し、fail には直し方を付ける。1 つの誤りで残りの検査を止めない。
fail があれば非 0 で終える。<repo> を省くと、環境と user 設定だけを見る。sandbox VM の状態 (drift など) は見ない。`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			userConfig, err := deps.userConfigPath()
			if err != nil {
				return err
			}
			secretFile, err := deps.secretFilePath()
			if err != nil {
				return err
			}
			d := doctor.Doctor{
				Runtime:      deps.runtime,
				SbxAvailable: deps.sbxAvailable,
				Herdr:        deps.herdr,
				UserConfig:   userConfig,
				SecretFile:   secretFile,
				Clone:        deps.clone,
			}
			var report doctor.Report
			if len(args) == 0 {
				report = d.Diagnose(cmd.Context())
			} else {
				report = d.DiagnoseRepo(cmd.Context(), args[0])
			}
			printReport(cmd, report)
			if failures := report.Count(doctor.Fail); failures > 0 {
				return fmt.Errorf("診断で fail が %d 件ある", failures)
			}
			return nil
		},
	}
}

// printReport は項目ごとに状態・名前・説明を 1 行で、fail には直し方を次の行に表示する。説明が複数行なら続きを字下げする。
func printReport(cmd *cobra.Command, report doctor.Report) {
	const indent = "        "
	for _, section := range report.Sections {
		printf(cmd, "%s\n", section.Title)
		for _, item := range section.Items {
			detail := strings.ReplaceAll(item.Detail, "\n", "\n"+indent+"  ")
			printf(cmd, "  %-4s  %s: %s\n", item.Status, item.Name, detail)
			if item.Fix != "" {
				printf(cmd, "%s直し方: %s\n", indent, item.Fix)
			}
		}
	}
	printf(cmd, "ok %d / fail %d / skip %d\n", report.Count(doctor.OK), report.Count(doctor.Fail), report.Count(doctor.Skip))
}
