// Package doctor は host の前提と 3 スコープの宣言を、規則に照らして全件検査する (診断。decision/0012)。
// スコープごとに独立して検査し、ある項目の失敗で他の項目を止めない。検査に要る入力が他の項目の失敗で得られないときだけ skip にする。
// host の管理状態・sandbox VM・global rule のどれも変えない。
package doctor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/egress"
	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/sandbox"
	"github.com/swat9013/sbxr/internal/secret"
)

// Status は 1 つの検査項目の結果。
type Status int

const (
	// OK は検査を通った。
	OK Status = iota + 1
	// Fail は検査を通らなかった。直し方を持つ。
	Fail
	// Skip は、検査に要る入力が他の項目の失敗で得られないか、検査の対象が無いので確かめていない。
	Skip
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Fail:
		return "fail"
	case Skip:
		return "skip"
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// Item は 1 つの検査項目の結果。
type Item struct {
	Name   string
	Status Status
	// Detail は結果の説明。Fail では何が・なぜ駄目か、Skip では確かめていない理由。
	Detail string
	// Fix は Fail の直し方 (1 行)。Fail 以外では空。
	Fix string
}

// Section は検査項目のまとまり (環境・user スコープ・repo スコープ・merge 後)。
type Section struct {
	Title string
	Items []Item
}

// Report は診断の結果。
type Report struct {
	Sections []Section
}

// Count は status の項目の数。
func (r Report) Count(status Status) int {
	count := 0
	for _, section := range r.Sections {
		for _, item := range section.Items {
			if item.Status == status {
				count++
			}
		}
	}
	return count
}

// Doctor は診断に使う host の前提と宣言ファイルの置き場。
type Doctor struct {
	Runtime runtime.Runtime
	// SbxAvailable は host に sbx があるかを確かめる。無ければ error。
	SbxAvailable func() error
	Herdr        herdr.Client
	UserConfig   string
	SecretFile   string
	// Clone は git URL の repo を一時ディレクトリへ clone する。
	Clone sandbox.Cloner
}

// skip の理由。検査に要る入力を、他の項目の失敗で得られなかった。
const (
	reasonUserInvalid  = "user 設定が通らないので確かめていない"
	reasonRepoInvalid  = "repo 宣言が通らないので確かめていない"
	reasonMergeInvalid = "宣言を重ねられないので確かめていない"
)

// Diagnose は環境と user スコープを検査し、git identity と secret を user 設定だけで判定する。
func (d Doctor) Diagnose(ctx context.Context) Report {
	facts := d.readHostAndUser()
	if facts.userErr != nil {
		return d.report(ctx, facts, skippedMerge("merge 後 (default と user 設定)", reasonUserInvalid))
	}
	return d.report(ctx, facts, d.mergedSection(facts, judgement{
		title:    "merge 後 (default と user 設定)",
		config:   facts.trusted,
		unwired:  unwiredMayWireInRepo,
		identity: identityFromUser,
	}))
}

// DiagnoseRepo は Diagnose の項目に repo スコープを加え、git identity と secret を repo 宣言まで merge した結果で判定する。
// repo は plan と同じく path か git URL (一時ディレクトリへ clone して読み、読み終えたら消す)。
func (d Doctor) DiagnoseRepo(ctx context.Context, repo string) Report {
	facts := d.readHostAndUser()
	repoSection, repoFile, repoOK := d.repoScope(ctx, repo)
	const title = "merge 後 (default・user 設定・repo 宣言)"
	switch {
	case facts.userErr != nil:
		return d.report(ctx, facts, repoSection, skippedMerge(title, reasonUserInvalid))
	case !repoOK:
		// user 設定の secret 要求の値は repo に依らないので、repo を省いたときと同じく user 設定だけで判定する
		return d.report(ctx, facts, repoSection, d.mergedSection(facts, judgement{
			title:    "merge 後 (default と user 設定。repo 宣言は通らないので重ねていない)",
			config:   facts.trusted,
			unwired:  unwiredMayWireInRepo,
			identity: identityNeedsRepo,
		}))
	}
	cfg, err := facts.user.With(repoFile)
	if err != nil {
		// merge の検証は、user 設定 (Trusted) と repo の egress (SandboxEgress) の項目がすでに通している。
		// ここに来るのは merge に検証を足して項目を足し忘れたときなので、黙って ok にせず fail で見せる
		repoSection.Items = append(repoSection.Items, failure("merge", err.Error(), "user 設定か repo 宣言で上の誤りを直す"))
		return d.report(ctx, facts, repoSection, skippedMerge(title, reasonMergeInvalid))
	}
	return d.report(ctx, facts, repoSection, d.mergedSection(facts, judgement{
		title:    title,
		config:   cfg,
		unwired:  unwiredFails,
		identity: identityFromUserOrRepo,
	}))
}

// report は環境と user スコープの節に、入口ごとの節 (repo スコープと merge 後) を続ける。
func (d Doctor) report(ctx context.Context, facts hostAndUser, sections ...Section) Report {
	return Report{Sections: append([]Section{d.environment(facts), d.userScope(ctx, facts)}, sections...)}
}

// hostAndUser は、環境と user スコープの検査の入力。どの項目も、ここにある結果だけを見て状態を決める。
type hostAndUser struct {
	sbxErr  error
	user    config.UserFile
	trusted config.Config
	// userErr は user 設定をファイルとして読めないか、default に重ねられない error。
	userErr      error
	secretValues secret.Values
	secretErr    error
}

func (d Doctor) readHostAndUser() hostAndUser {
	facts := hostAndUser{sbxErr: d.SbxAvailable()}
	facts.user, facts.userErr = config.ReadUserFile(d.UserConfig)
	if facts.userErr == nil {
		facts.trusted, facts.userErr = facts.user.Trusted()
	}
	facts.secretValues, facts.secretErr = secret.ReadFile(d.SecretFile)
	return facts
}

func (d Doctor) environment(facts hostAndUser) Section {
	sbxItem := ok("sbx", "PATH にある")
	if facts.sbxErr != nil {
		sbxItem = failure("sbx", facts.sbxErr.Error(), "Docker Sandboxes の sbx を入れて PATH に置く (https://docs.docker.com/ai/sandboxes/)")
	}
	return Section{Title: "環境", Items: []Item{sbxItem, d.secretFileItem(facts), d.herdrItem(facts)}}
}

func (d Doctor) secretFileItem(facts hostAndUser) Item {
	const name = "secret ファイル"
	switch {
	case facts.secretErr == nil:
		return ok(name, fmt.Sprintf("%s を読める (key %d 個。ファイルが無ければ 0 個)", d.SecretFile, len(facts.secretValues)))
	case errors.Is(facts.secretErr, secret.ErrPermissiveMode):
		return failure(name, facts.secretErr.Error(), "chmod 600 "+d.SecretFile)
	case errors.Is(facts.secretErr, secret.ErrMalformedLine):
		return failure(name, facts.secretErr.Error(), d.SecretFile+" の上に挙げた行を KEY=VALUE の形に直す (# で始まる行と空行は読まない)")
	}
	return failure(name, facts.secretErr.Error(), d.SecretFile+" を自分の user で読めるようにする (持ち主と置き場のディレクトリの権限を確かめる)")
}

func (d Doctor) herdrItem(facts hostAndUser) Item {
	const name = "herdr"
	switch {
	case facts.userErr != nil:
		return skip(name, "user 設定が通らないので、herdr 連携が有効かを決められない")
	case !facts.trusted.Herdr.Enabled:
		return skip(name, "herdr 連携は無効")
	}
	if err := herdr.RequireOnHost(d.Herdr); err != nil {
		return failure(name, err.Error(), "herdr を入れて PATH に置くか、user 設定の herdr.enabled を false にする")
	}
	return ok(name, "herdr 連携が有効で、host に herdr がある")
}

func (d Doctor) userScope(ctx context.Context, facts hostAndUser) Section {
	var userItem Item
	switch {
	case facts.userErr != nil:
		userItem = failure("user 設定", facts.userErr.Error(), "user 設定 ("+d.UserConfig+") の上に挙げた key を直す")
	case facts.user.Declared():
		userItem = ok("user 設定", d.UserConfig+" は書式・型・key の検証を通る")
	default:
		userItem = ok("user 設定", d.UserConfig+" は無い (同梱の default だけを使う)")
	}
	return Section{Title: "user スコープ", Items: []Item{userItem, d.globalRuleItem(ctx, facts)}}
}

func (d Doctor) globalRuleItem(ctx context.Context, facts hostAndUser) Item {
	const name = "global rule"
	switch {
	case facts.sbxErr != nil:
		return skip(name, "sbx が無いので確かめていない")
	case facts.userErr != nil:
		return skip(name, reasonUserInvalid)
	}
	desired := facts.trusted.GlobalEgress
	changes, err := egress.Diff(ctx, d.Runtime, desired)
	if err != nil {
		return failure(name, err.Error(), "sbx が動くかを sbx policy ls で確かめる")
	}
	if !changes.Empty() {
		var excess []string
		for _, rule := range changes.Remove {
			excess = append(excess, fmt.Sprintf("%s (%s: %s)", rule.ID, rule.Decision, strings.Join(rule.Resources, ", ")))
		}
		detail := fmt.Sprintf("宣言と一致しない (余分な rule: %s / 足りない宛先: %s)", listOrNone(excess), listOrNone(changes.Add))
		return failure(name, detail, "sbxr policy sync で宣言に揃える")
	}
	return ok(name, fmt.Sprintf("宣言と一致している (宛先 %d)", len(desired)))
}

func listOrNone(entries []string) string {
	if len(entries) == 0 {
		return "無し"
	}
	return strings.Join(entries, ", ")
}

// repoScope は repo 宣言をファイルとして検査し、repo の egress の group が揃っているかを検査する。
// repoOK は repo 宣言を merge に使えるか (どちらの項目も通った)。
func (d Doctor) repoScope(ctx context.Context, repo string) (section Section, repoFile config.RepoFile, repoOK bool) {
	section.Title = "repo スコープ"
	repoFile, err := sandbox.ReadRepoDeclaration(ctx, d.Clone, repo)
	if err != nil {
		fix := "repo 宣言 (sbxr.yaml) の上に挙げた key を直す"
		if unreadable := (*sandbox.RepoUnreadableError)(nil); errors.As(err, &unreadable) {
			fix = "上の理由を直す (repo のディレクトリ名は sandbox VM の名前になるので英数字と . _ - で書く。git URL は clone できるものを渡す)"
		}
		section.Items = append(section.Items, failure("repo 宣言", err.Error(), fix), skip("repo の egress", reasonRepoInvalid))
		return section, config.RepoFile{}, false
	}
	if repoFile.Declared() {
		section.Items = append(section.Items, ok("repo 宣言", repo+" の sbxr.yaml は書式・型・key・スコープ制限の検証を通る"))
	} else {
		section.Items = append(section.Items, ok("repo 宣言", repo+" に sbxr.yaml は無い (repo 宣言の無い repo として扱う)"))
	}
	resources, err := repoFile.SandboxEgress()
	if err != nil {
		section.Items = append(section.Items, failure("repo の egress", err.Error(), "repo 宣言の egress の各 group に rationale と allow を書く"))
		return section, config.RepoFile{}, false
	}
	section.Items = append(section.Items, ok("repo の egress", fmt.Sprintf("group が揃っている (sandbox スコープ rule の宛先 %d)", len(resources))))
	return section, repoFile, true
}

// judgement は、merge 後の節で git identity と secret をどの宣言でどう判定するか。
type judgement struct {
	title    string
	config   config.Config
	unwired  unwiredSecret
	identity identitySource
}

// unwiredSecret は、注入先 host が egress で許可されていないので配線されない secret をどう判定するか。
type unwiredSecret int

const (
	// unwiredMayWireInRepo は repo 宣言を重ねていない判定。repo の egress で許可されれば配線されるので skip にする。
	unwiredMayWireInRepo unwiredSecret = iota + 1
	// unwiredFails は repo 宣言まで重ねた判定。create はこの secret を配線せずに進むが、要求と egress の食い違いなので fail にする。
	unwiredFails
)

// identitySource は git identity を、どの宣言から判定するか。
type identitySource int

const (
	// identityFromUser は user 設定だけで判定する (repo を渡していない)。
	identityFromUser identitySource = iota + 1
	// identityFromUserOrRepo は repo 宣言まで重ねて判定する。
	identityFromUserOrRepo
	// identityNeedsRepo は repo を渡したが repo 宣言が通らないので判定しない (repo 宣言が git identity を持ちうる)。
	identityNeedsRepo
)

// skippedMerge は merge 後の宣言を得られないときの節。
func skippedMerge(title, reason string) Section {
	return Section{Title: title, Items: []Item{skip("git identity", reason), skip("secret", reason)}}
}

// mergedSection は git identity と secret を j の宣言で判定する。
func (d Doctor) mergedSection(facts hostAndUser, j judgement) Section {
	return Section{Title: j.title, Items: append([]Item{d.identityItem(j)}, d.secretItems(j, facts)...)}
}

func (d Doctor) identityItem(j judgement) Item {
	const name = "git identity"
	fix := "user 設定 (" + d.UserConfig + ") に git.name と git.email を書く"
	switch j.identity {
	case identityNeedsRepo:
		return skip(name, reasonRepoInvalid)
	case identityFromUserOrRepo:
		fix = "user 設定 (" + d.UserConfig + ") か repo 宣言 (sbxr.yaml) に git.name と git.email を書く"
	case identityFromUser:
	}
	identity, err := j.config.GitIdentity()
	if err != nil {
		return failure(name, err.Error(), fix)
	}
	return ok(name, fmt.Sprintf("%s <%s>", identity.Name, identity.Email))
}

// secretItems は要求された secret ごとに、create と同じ規則で配線されるか、配線されるなら値が secret ファイルにあるかを検査する。
func (d Doctor) secretItems(j judgement, facts hostAndUser) []Item {
	cfg := j.config
	if len(cfg.Secrets) == 0 {
		return []Item{ok("secret", "要求された secret は無い")}
	}
	allowed := append(slices.Clone(cfg.GlobalEgress), cfg.SandboxEgress...)
	var items []Item
	var defined []string
	for _, name := range cfg.Secrets {
		items = append(items, d.secretItem(name, j.unwired, cfg.SecretDefs, allowed, facts))
		if _, ok := cfg.SecretDefs[name]; ok {
			defined = append(defined, name)
		}
	}
	// 付随値 (vars) の食い違いは 1 つの secret では決まらない。create は配線する secret を合わせて止める。
	// defined は定義のある名前だけなので、PlanWiring は error を返さない
	if plan, err := secret.PlanWiring(defined, cfg.SecretDefs, allowed); err == nil {
		if _, err := plan.VMEnv(); err != nil {
			items = append(items, failure("secret の vars", err.Error(), "user 設定の secret_defs で、同じ vars の名前を 1 つの値に揃える"))
		}
	}
	return items
}

func (d Doctor) secretItem(name string, unwired unwiredSecret, defs map[string]secret.Definition, allowed []string, facts hostAndUser) Item {
	item := "secret " + name
	plan, err := secret.PlanWiring([]string{name}, defs, allowed)
	if err != nil {
		return failure(item, err.Error(), "user 設定の secret_defs に "+name+" を定義するか、secrets から外す")
	}
	if len(plan.Skipped) > 0 {
		hosts := strings.Join(plan.Skipped[0].DeniedHosts, ", ")
		if unwired == unwiredMayWireInRepo {
			return skip(item, "注入先 host "+hosts+" は global rule では許可されていない。repo の egress で許可されれば配線されるので、sbxr doctor <repo> で確かめる")
		}
		return failure(item, "注入先 host "+hosts+" が egress で許可されていないので配線されない",
			"egress の group の allow で "+hosts+" への HTTPS を許可するか、secrets から "+name+" を外す")
	}
	if facts.secretErr != nil {
		return skip(item, "配線される。secret ファイルを読めないので値は確かめていない")
	}
	if err := plan.RequireValues(facts.secretValues); err != nil {
		return failure(item, err.Error(), d.secretSetupFix(name, defs))
	}
	return ok(item, "配線され、値が secret ファイルにある")
}

// secretSetupFix は secret の値を書く手順。書く sbxr のコマンドが無い定義は、secret ファイルへ直接書く手順にする。
func (d Doctor) secretSetupFix(name string, defs map[string]secret.Definition) string {
	if command, found := setupCommand(name, defs); found {
		return command + " で値を書く"
	}
	return fmt.Sprintf("secret ファイル (%s) に %s=<値> の行を書く (mode 0600)", d.SecretFile, defs[name].Key)
}

// setupCommand は secret 定義 name の値を secret ファイルへ書く sbxr secret setup のコマンドを返す。
// 書けるコマンドが無い定義 (github 以外の sbx 組み込み service、注入先 host から key が 1 つに決まらない placeholder 注入) は found が false。
func setupCommand(name string, defs map[string]secret.Definition) (command string, found bool) {
	if name == secret.GitHubName {
		return "sbxr secret setup github", true
	}
	def := defs[name]
	if !def.InjectsPlaceholder() {
		return "", false
	}
	for _, host := range def.Hosts {
		// setup custom は host から key を引くので、その key がこの定義のものに決まる host だけを案内する
		if key, err := secret.PlaceholderKeyForHost(defs, host); err == nil && key == def.Key {
			return "sbxr secret setup custom --host " + host, true
		}
	}
	return "", false
}

func ok(name, detail string) Item {
	return Item{Name: name, Status: OK, Detail: detail}
}

func skip(name, reason string) Item {
	return Item{Name: name, Status: Skip, Detail: reason}
}

func failure(name, detail, fix string) Item {
	return Item{Name: name, Status: Fail, Detail: detail, Fix: fix}
}
