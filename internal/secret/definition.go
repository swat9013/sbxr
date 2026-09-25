package secret

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"

	"go.yaml.in/yaml/v3"
)

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

// hostPattern は注入先 host の書式。egress の許可を一意に判定できるよう、glob と port は書けない。
var hostPattern = regexp.MustCompile(`^([a-z0-9-]+\.)+[a-z0-9-]+$`)

// ParseDefinitions は config が中身を検査せずに持つ secret_defs を Definition へ読み、検証する。
func ParseDefinitions(raw map[string]map[string]any) (map[string]Definition, error) {
	defs := make(map[string]Definition, len(raw))
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		def, err := parseDefinition(raw[name])
		if err == nil {
			err = def.validate()
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("secret_defs.%s: %w", name, err))
			continue
		}
		defs[name] = def
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return defs, nil
}

func parseDefinition(raw map[string]any) (Definition, error) {
	// 未知の field を error にするため、YAML に戻して KnownFields で読み直す
	data, err := yaml.Marshal(raw)
	if err != nil {
		return Definition{}, err
	}
	var def Definition
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&def); err != nil {
		return Definition{}, err
	}
	return def, nil
}

func (d Definition) validate() error {
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
	case d.Service != "" && d.Env != "":
		errs = append(errs, errors.New("service と env は両方書けない (env は placeholder 注入だけが使う。service の環境変数は sbx が決める)"))
	case d.Service == "" && !keyPattern.MatchString(d.Env):
		errs = append(errs, fmt.Errorf("env %q は VM の環境変数名で書く (placeholder 注入には env が要る)", d.Env))
	}
	for _, name := range slices.Sorted(maps.Keys(d.Vars)) {
		if !keyPattern.MatchString(name) {
			errs = append(errs, fmt.Errorf("vars の %q は VM の環境変数名で書く", name))
		}
	}
	return errors.Join(errs...)
}
