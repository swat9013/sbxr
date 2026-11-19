package sandbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/inmemory"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

// answerProbes は probe script に、allowed に無い example.com・example.net・example.org には拒否を、ほかの宛先には 200 を返す
// (global policy が default deny で、global rule が allowed の実行基盤)。拒否は、VM 直下には proxy の拒否応答、
// proxy を使わない container には名前解決の拒否 (curl の exit 6) で返す。container の image の pull は通す。
func answerProbes(allowed []string) func(runtime.SandboxCommand) ([]byte, error) {
	return func(command runtime.SandboxCommand) ([]byte, error) {
		url, fromContainer, ok := probedURL(command)
		if !ok {
			return nil, nil
		}
		host := strings.TrimSuffix(strings.TrimPrefix(url, "https://"), "/")
		if slices.Contains([]string{"example.com", "example.net", "example.org"}, host) && !slices.Contains(allowed, host+":443") {
			if fromContainer {
				return []byte("6 000\ncurl: (6) Could not resolve host: " + host + "\n"), nil
			}
			return []byte("0 403\n" + approvalRequired(host)), nil
		}
		return []byte("0 200\nok\n"), nil
	}
}

// probedURL は command が probe script なら、その宛先と、container から送ったかを返す。
func probedURL(command runtime.SandboxCommand) (url string, fromContainer, ok bool) {
	args := command.Args
	if len(args) < 3 || args[len(args)-3] != probeScript {
		return "", false, false
	}
	return args[len(args)-1], args[0] == "docker", true
}

// pullsImage は command が container の image の pull か。
func pullsImage(command runtime.SandboxCommand) bool {
	return slices.Equal(command.Args, []string{"docker", "pull", probeImage})
}

// pulled は commands に container の image の pull があるか。
func pulled(commands []inmemory.Command) bool {
	return slices.ContainsFunc(commands, func(command inmemory.Command) bool { return pullsImage(command.SandboxCommand) })
}

// approvalRequired は sbx の proxy が、どの rule にも当たらない宛先へ返す body (実測は sbx stub が持つ)。
func approvalRequired(host string) string {
	return sbxstub.ProxyDenial(host).Body
}

func TestProbeReadsOnlyTheProxysDenialOfThatHostAsDenied(t *testing.T) {
	for _, tt := range []struct {
		name, out string
		want      probeResult
	}{
		{"承認待ち (どの rule にも当たらない)", "0 403\n" + approvalRequired("example.com"), probeDenied},
		{"deny rule に当たった", "0 403\nBlocked by local rule for example.com:443\n", probeDenied},
		{"別の宛先の承認待ち", "0 403\n" + approvalRequired("example.net"), probeReached},
		{"別の宛先の deny rule", "0 403\nBlocked by local rule for example.net:443\n", probeReached},
		{"別の port の deny rule", "0 403\nBlocked by local rule for example.com:4430\n", probeReached},
		{"宛先自身の 403", "0 403\n<html>Forbidden</html>\n", probeReached},
		{"sbx v0.45.1 の拒否応答は受けない", "0 403\nBlocked by network policy: domain example.com:443\n", probeReached},
		{"200 で同じ文言の body", "0 200\n" + approvalRequired("example.com"), probeReached},
		{"HTTP 応答が無い", "28 000\ncurl: (28) Connection timed out\n", probeFailed},
		{"名前解決の拒否", "6 000\ncurl: (6) Could not resolve host\n", probeCutOff},
		{"TLS の切断", "35 000\ncurl: (35) Recv failure: Connection reset by peer\n", probeCutOff},
		{"空の応答", "52 000\ncurl: (52) Empty reply from server\n", probeCutOff},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := runningVM(t, func(runtime.SandboxCommand) ([]byte, error) { return []byte(tt.out), nil })

			p, err := probeHTTPS(context.Background(), v, fromVM, "example.com")

			if err != nil || p.result != tt.want {
				t.Errorf("probeHTTPS = %+v, %v, want result %d", p, err, tt.want)
			}
		})
	}
}

// probedURLs は probe script を送った宛先を、送った順に返す。container から送ったものは "container " を前に付け、
// container の image の pull は "pull" とする。
func probedURLs(rt *inmemory.Runtime) []string {
	var urls []string
	for _, command := range rt.Commands {
		if pullsImage(command.SandboxCommand) {
			urls = append(urls, "pull")
		}
		url, fromContainer, ok := probedURL(command.SandboxCommand)
		switch {
		case ok && fromContainer:
			urls = append(urls, "container "+url)
		case ok:
			urls = append(urls, url)
		}
	}
	return urls
}

// fakeCurl は -o の file へ body を書き、-w の代わりに状態コードを出して exit code で終える curl を PATH に置く。
func fakeCurl(t *testing.T, body, code string, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
  esac
  shift
done
printf '%s' "$FAKE_BODY" > "$out"
if [ "$FAKE_EXIT" -ne 0 ]; then echo "curl: (6) Could not resolve host" >&2; fi
printf '%s' "$FAKE_CODE"
exit "$FAKE_EXIT"
`
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_BODY", body)
	t.Setenv("FAKE_CODE", code)
	t.Setenv("FAKE_EXIT", strconv.Itoa(exitCode))
}

func TestProbeScriptPrintsTheCurlExitCodeTheStatusAndTheBodyHead(t *testing.T) {
	for _, tt := range []struct {
		name, body, code string
		exitCode         int
		want             string
	}{
		{"proxy の拒否応答", "Approval required for example.com:443.\n", "403", 0, "0 403\nApproval required for example.com:443.\n"},
		{"curl の失敗は error を出す", "", "000", 6, "6 000\ncurl: (6) Could not resolve host\n"},
		{"body は先頭 1024 byte だけ", strings.Repeat("x", 2000), "200", 0, "0 200\n" + strings.Repeat("x", 1024)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeCurl(t, tt.body, tt.code, tt.exitCode)

			out, err := exec.Command("sh", "-c", probeScript, "sh", "https://example.com/").Output()

			if err != nil || string(out) != tt.want {
				t.Errorf("probe script = %q, %v, want %q", out, err, tt.want)
			}
		})
	}
}

func TestCheckEgressSkipsTheSideWithoutAProbeTarget(t *testing.T) {
	for _, tt := range []struct {
		name       string
		global     []string
		wantProbes []string
		wantSkip   string
	}{
		{"glob を含まず 443 を通す許可先が無い", []string{"**.github.com:443", "ports.ubuntu.com:80"},
			[]string{"https://example.com/", "pull", "container https://example.com/"}, "VM の中の container から、許可先に届くかの確認を省いた"},
		{"許可外の候補がすべて許可されている", []string{"example.com:443", "example.net:443", "example.org:443"},
			[]string{"https://example.com/", "pull", "container https://example.com/"}, "VM の中の container から、許可外に届かないかの確認を省いた"},
		{"両側とも probe 先が無いと image も pull しない", []string{"**:443"}, nil, "VM の中の container からの確認を省いた"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v, rt := runningVM(t, answerProbes(tt.global))
			var progress bytes.Buffer

			err := checkEgress(context.Background(), v, preparation{GlobalEgress: tt.global}, &progress)

			if err != nil {
				t.Errorf("checkEgress() error = %v, want the remaining side to pass", err)
			}
			if probes := probedURLs(rt); !slices.Equal(probes, tt.wantProbes) {
				t.Errorf("probes = %q, want %q", probes, tt.wantProbes)
			}
			if !strings.Contains(progress.String(), tt.wantSkip) {
				t.Errorf("progress = %q, want %q", progress.String(), tt.wantSkip)
			}
		})
	}
}

// checkEgress は egress 自己検証を、create と同じく VM 直下 → container の順に行う (間の agent と Docker の確認は省く)。
func checkEgress(ctx context.Context, v vm, prepared preparation, progress io.Writer) error {
	targets := probeTargetsOf(prepared)
	if err := checkEgressFromVM(ctx, v, targets, progress); err != nil {
		return err
	}
	return checkEgressFromContainer(ctx, v, targets, progress)
}

// allowsGithubOnly は許可先 github.com:443 だけを持ち、example.com を許可しない許可集合。
var allowsGithubOnly = []string{"github.com:443"}

// answerContainer は container からの probe script にだけ out を返し、ほかは answerProbes(allowsGithubOnly) に任せる。
func answerContainer(host, out string) func(runtime.SandboxCommand) ([]byte, error) {
	probes := answerProbes(allowsGithubOnly)
	return func(command runtime.SandboxCommand) ([]byte, error) {
		if url, fromContainer, ok := probedURL(command); ok && fromContainer && url == "https://"+host+"/" {
			return []byte(out), nil
		}
		return probes(command)
	}
}

func TestCheckEgressProbesFromAContainerInTheVMAfterTheVMWithAPulledImage(t *testing.T) {
	v, rt := runningVM(t, answerProbes(allowsGithubOnly))
	var progress bytes.Buffer

	err := checkEgress(context.Background(), v, preparation{GlobalEgress: allowsGithubOnly}, &progress)

	want := []string{"https://github.com/", "https://example.com/", "pull", "container https://github.com/", "container https://example.com/"}
	if probes := probedURLs(rt); err != nil || !slices.Equal(probes, want) {
		t.Errorf("checkEgress() error = %v, probes = %q, want %q", err, probes, want)
	}
	for _, want := range []string{"VM の中の container に使う image curlimages/curl:8.11.1 を pull する", "VM の中の container から、許可先 github.com:443 に届いた", "VM の中の container から、許可外の example.com:443 は proxy が拒否した (curl の exit 6)"} {
		if !strings.Contains(progress.String(), want) {
			t.Errorf("progress = %q, want %q", progress.String(), want)
		}
	}
}

func TestCheckEgressRunsTheContainerWithoutAProxyFromAFixedImageTag(t *testing.T) {
	v, rt := runningVM(t, answerProbes(allowsGithubOnly))

	if err := checkEgress(context.Background(), v, preparation{GlobalEgress: allowsGithubOnly}, io.Discard); err != nil {
		t.Fatal(err)
	}

	for _, command := range rt.Commands {
		if _, fromContainer, ok := probedURL(command.SandboxCommand); ok && fromContainer {
			if want := []string{"docker", "run", "--rm", "--entrypoint", "sh", "curlimages/curl:8.11.1", "-c"}; !slices.Equal(command.Args[:7], want) || slices.ContainsFunc(command.Args, isEnvFlag) {
				t.Errorf("container probe = %q, want it to start with %q and pass no proxy variable", command.Args, want)
			}
		}
	}
}

// isEnvFlag は docker run に環境変数を渡す flag か。
func isEnvFlag(arg string) bool {
	return arg == "-e" || arg == "--env" || arg == "--env-file" || strings.HasPrefix(arg, "--env=")
}

func TestCheckEgressFromTheContainerReadsTheTransparentDenialsAsDenied(t *testing.T) {
	for _, out := range []string{
		"6 000\ncurl: (6) Could not resolve host: example.com\n",
		"35 000\ncurl: (35) Recv failure: Connection reset by peer\n",
		"52 000\ncurl: (52) Empty reply from server\n",
		"0 403\n" + approvalRequired("example.com"),
	} {
		t.Run(out, func(t *testing.T) {
			v, _ := runningVM(t, answerContainer("example.com", out))

			if err := checkEgress(context.Background(), v, preparation{GlobalEgress: allowsGithubOnly}, io.Discard); err != nil {
				t.Errorf("checkEgress() error = %v, want the denial from the container to pass", err)
			}
		})
	}
}

func TestCheckEgressFromTheContainerStopsUnlessTheDisallowedHostIsDenied(t *testing.T) {
	for name, tt := range map[string]struct{ out, want string }{
		"宛先の応答":     {"0 200\n<html>\n", "VM の中の container から、許可外の example.com:443 に届いた"},
		"拒否の形でない失敗": {"28 000\ncurl: (28) Connection timed out\n", "VM の中の container から、許可外の example.com:443 への通信が proxy の拒否応答も、拒否と読む curl の exit (6・35・52) も得ずに失敗した"},
	} {
		t.Run(name, func(t *testing.T) {
			v, _ := runningVM(t, answerContainer("example.com", tt.out))

			err := checkEgress(context.Background(), v, preparation{GlobalEgress: allowsGithubOnly}, io.Discard)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("checkEgress() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCheckEgressFromTheContainerStopsWhenTheAllowedHostIsNotReached(t *testing.T) {
	for _, out := range []string{
		"6 000\ncurl: (6) Could not resolve host: github.com\n",
		"28 000\ncurl: (28) Connection timed out\n",
		"0 403\nApproval required for github.com:443.\n",
	} {
		t.Run(out, func(t *testing.T) {
			v, _ := runningVM(t, answerContainer("github.com", out))

			err := checkEgress(context.Background(), v, preparation{GlobalEgress: allowsGithubOnly}, io.Discard)

			if err == nil || !strings.Contains(err.Error(), "VM の中の container から、許可先 github.com:443 に届かない") {
				t.Errorf("checkEgress() error = %v, want the allowed host unreached from the container", err)
			}
		})
	}
}

func TestCheckEgressFromTheVMStillFailsOnTheTransparentDenialShape(t *testing.T) {
	probes := answerProbes(allowsGithubOnly)
	v, rt := runningVM(t, func(command runtime.SandboxCommand) ([]byte, error) {
		if url, fromContainer, ok := probedURL(command); ok && !fromContainer && url == "https://example.com/" {
			return []byte("6 000\ncurl: (6) Could not resolve host: example.com\n"), nil
		}
		return probes(command)
	})

	err := checkEgress(context.Background(), v, preparation{GlobalEgress: allowsGithubOnly}, io.Discard)

	if err == nil || !strings.Contains(err.Error(), "許可外の example.com:443 への通信が proxy の拒否応答を得ずに失敗した") {
		t.Errorf("checkEgress() error = %v, want the VM side to require the proxy's denial response", err)
	}
	if pulled(rt.Commands) {
		t.Errorf("the image was pulled although the check from the VM failed")
	}
}

func TestCheckEgressStopsNamingTheDockerRegistryGroupWhenTheImageCannotBePulled(t *testing.T) {
	probes := answerProbes(allowsGithubOnly)
	v, rt := runningVM(t, func(command runtime.SandboxCommand) ([]byte, error) {
		if pullsImage(command) {
			return nil, errors.New("Error response from daemon: Get \"https://registry-1.docker.io/v2/\": Forbidden")
		}
		return probes(command)
	})

	err := checkEgress(context.Background(), v, preparation{GlobalEgress: allowsGithubOnly}, io.Discard)

	if err == nil || !strings.Contains(err.Error(), "image curlimages/curl:8.11.1 を pull できない") || !strings.Contains(err.Error(), "docker-registry group が要る") {
		t.Errorf("checkEgress() error = %v, want the pull failure naming the docker-registry group", err)
	}
	if probes := probedURLs(rt); slices.ContainsFunc(probes, func(p string) bool { return strings.HasPrefix(p, "container ") }) {
		t.Errorf("probes = %q, want no probe from a container without the image", probes)
	}
}
