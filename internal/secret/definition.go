package secret

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// GitHubName は同梱の default 宣言が持つ GitHub の secret 定義の名前 (sbxr secret setup github が書く)。
const GitHubName = "github"

// Definition は secret 定義 (secret_defs.<name>)。default と user スコープだけが書ける (ADR 0003)。
type Definition struct {
	// Service は sbx 組み込み service の名前 (例: github)。空なら Hosts への placeholder 注入。
	Service string `yaml:"service"`
	// Key は secret ファイルのキー。
	Key string `yaml:"key"`
	// Hosts は注入先 host。すべてが egress で許可されているときだけ配線する。
	// sbx 組み込み service は注入先を sbx が決めるので、ここには egress で許可を確かめる host を書く。
	Hosts []string `yaml:"hosts"`
	// Env は placeholder を入れる VM の環境変数名。placeholder 注入だけが書く。
	Env string `yaml:"env"`
	// Vars は secret と一緒に VM へ渡す、秘密でない付随値 (環境変数名 → 値)。
	Vars map[string]string `yaml:"vars"`
}

// InjectsPlaceholder は host 指定の placeholder 注入か (sbx 組み込み service でないか) を返す。
func (d Definition) InjectsPlaceholder() bool {
	return d.Service == ""
}

// hostPattern は注入先 host の書式。egress の許可を一意に判定できるよう、glob と port は書けない。
var hostPattern = regexp.MustCompile(`^([a-z0-9-]+\.)+[a-z0-9-]+$`)

// Validate は 1 つの secret 定義を検証する。
func (d Definition) Validate() error {
	var errs []error
	if !keyPattern.MatchString(d.Key) {
		errs = append(errs, fmt.Errorf("key %q は secret ファイルのキー (英数字と _) で書く", d.Key))
	}
	if len(d.Hosts) == 0 {
		errs = append(errs, errors.New("hosts が空 (注入先 host を書く)"))
	}
	for _, host := range d.Hosts {
		if !hostPattern.MatchString(host) {
			errs = append(errs, fmt.Errorf("hosts の %q は小文字の host 名で書く (glob・port は書けない)", host))
		}
	}
	switch {
	case !d.InjectsPlaceholder() && d.Env != "":
		errs = append(errs, errors.New("service と env は両方書けない (env は placeholder 注入だけが使う。service の環境変数は sbx が決める)"))
	case d.InjectsPlaceholder() && !keyPattern.MatchString(d.Env):
		errs = append(errs, fmt.Errorf("env %q は VM の環境変数名で書く (placeholder 注入には env が要る)", d.Env))
	}
	for _, name := range slices.Sorted(maps.Keys(d.Vars)) {
		if !keyPattern.MatchString(name) {
			errs = append(errs, fmt.Errorf("vars の %q は VM の環境変数名で書く", name))
		}
	}
	return errors.Join(errs...)
}

// PlaceholderKeyForHost は host へ placeholder 注入する secret 定義の key を返す。候補が 1 つに決まらなければ止める。
func PlaceholderKeyForHost(defs map[string]Definition, host string) (string, error) {
	keys := map[string][]string{} // key → それを使う定義の名前
	for _, name := range slices.Sorted(maps.Keys(defs)) {
		def := defs[name]
		if def.InjectsPlaceholder() && slices.Contains(def.Hosts, host) {
			keys[def.Key] = append(keys[def.Key], name)
		}
	}
	switch len(keys) {
	case 0:
		return "", fmt.Errorf("%s へ placeholder 注入する secret 定義が無い (user 設定の secret_defs に定義してから実行する)", host)
	case 1:
		for key := range keys {
			return key, nil
		}
	}
	return "", fmt.Errorf("%s へ注入する secret 定義の key が複数ある (%s)。1 つに揃える", host, strings.Join(slices.Sorted(maps.Keys(keys)), ", "))
}
