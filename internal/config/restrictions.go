package config

import (
	"errors"
	"fmt"
	"strings"
)

// repoAccess は repo 宣言 (untrusted) が key を書けるか。
type repoAccess int

const (
	// repoCannotWrite は repo が書けない key。
	repoCannotWrite repoAccess = iota
	// repoWritesKey は repo がこの key だけを書ける。子の key は、それぞれの行が許すときだけ書ける。
	repoWritesKey
	// repoWritesSubtree は repo がこの key とその下のすべてを書ける。
	repoWritesSubtree
)

// repoScopeTable はスコープ制限表。宣言の型が持つすべての key を分類する (分類の漏れは test が止める)。
// 表に無い key の既定は「repo は書けない」(key を足したときに fail-closed に倒れる。ADR 0004 の改訂)。
// 利用者が名前を付ける map の key は "*" で引く ("egress.*.allow")。
// default と user (信頼済み) は制限を持たない。repo の egress は Merge が sandbox スコープ rule として別に置く。
var repoScopeTable = map[string]repoAccess{
	"version":                repoWritesKey,
	"profile":                repoWritesKey,
	"profile.model":          repoWritesKey,
	"profile.effortLevel":    repoWritesKey,
	"profile.enabledPlugins": repoWritesSubtree,
	// base 固定の profile key
	"profile.language":                repoCannotWrite,
	"profile.outputStyle":             repoCannotWrite,
	"profile.feedbackDrafts":          repoCannotWrite,
	"profile.awaySummaryEnabled":      repoCannotWrite,
	"profile.autoMemoryEnabled":       repoCannotWrite,
	"profile.autoDreamEnabled":        repoCannotWrite,
	"profile.promptSuggestionEnabled": repoCannotWrite,
	"profile.spinnerTipsEnabled":      repoCannotWrite,
	"profile.env":                     repoCannotWrite,
	"profile.extraKnownMarketplaces":  repoCannotWrite,
	"git":                             repoWritesKey,
	"git.name":                        repoWritesKey,
	"git.email":                       repoWritesKey,
	"egress":                          repoWritesKey,
	"egress.*":                        repoWritesKey,
	"egress.*.rationale":              repoWritesKey,
	"egress.*.allow":                  repoWritesSubtree,
	// repo の egress は sandbox スコープ rule で、global rule の group を除外できない (ADR 0008)
	"egress.*.enabled": repoCannotWrite,
	"init":             repoWritesSubtree,
	"boot":             repoWritesSubtree,
	// 注入先 host を repo に指定させない (ADR 0003)
	"secret_defs": repoCannotWrite,
	"secrets":     repoWritesSubtree,
	// untrusted な repo に host の herdr へ machine を登録させない (ADR 0007)
	"herdr":         repoCannotWrite,
	"herdr.enabled": repoCannotWrite,
	"herdr.version": repoCannotWrite,
}

// repoCanWrite は repo が key (表で引く名前) を書けるかを返す。key の行が「この key だけ」か「この下は全部」なら書ける。
// 行が無ければ、祖先のどれかが「この下は全部」を許すときだけ書ける。
func repoCanWrite(tableName string) bool {
	if access, ok := repoScopeTable[tableName]; ok {
		return access != repoCannotWrite
	}
	for ancestor := parentKey(tableName); ancestor != ""; ancestor = parentKey(ancestor) {
		if access, ok := repoScopeTable[ancestor]; ok {
			return access == repoWritesSubtree
		}
	}
	return false
}

// parentKey は key の親 ("egress.*.allow" の親は "egress.*")。top-level の key なら空。
func parentKey(key string) string {
	i := strings.LastIndex(key, ".")
	if i < 0 {
		return ""
	}
	return key[:i]
}

// childTableName は親 (表で引く名前) の下の key を表で引く名前。表が親の下を "*" で引くなら、利用者が付けた名前を "*" にする。
func childTableName(parent, key string) string {
	if _, named := repoScopeTable[joinKey(parent, "*")]; named {
		return joinKey(parent, "*")
	}
	return joinKey(parent, key)
}

func joinKey(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func checkScopeRestrictions(scope Scope, keys []writtenKey) error {
	if scope != ScopeRepo {
		return nil
	}
	var errs []error
	for _, key := range keys {
		if !repoCanWrite(key.tableName) {
			errs = append(errs, fmt.Errorf("%s は repo 宣言には書けない", key.name()))
		}
	}
	return errors.Join(errs...)
}
