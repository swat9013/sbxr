package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// prompter は利用者から入力を受け取る。Hidden は入力を画面に出さない。Confirm は y/N を尋ね、端末が無ければ error を返す。
type prompter interface {
	Line(prompt string) (string, error)
	Hidden(prompt string) (string, error)
	Confirm(prompt string) (bool, error)
}

// terminalPrompter は端末から入力を読む。prompt は stderr に出す。
type terminalPrompter struct {
	stdin *bufio.Reader
}

func newTerminalPrompter() terminalPrompter {
	return terminalPrompter{stdin: bufio.NewReader(os.Stdin)}
}

func (p terminalPrompter) Line(prompt string) (string, error) {
	_, _ = fmt.Fprint(os.Stderr, prompt)
	line, err := p.stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("入力を読めない: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Confirm は y か yes (大小文字を問わない) だけを承認として読む。端末でなければ止める (確認なしに進めない)。
func (p terminalPrompter) Confirm(prompt string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, errors.New("確認には端末が要る (stdin が端末でない)")
	}
	answer, err := p.Line(prompt)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// Hidden は golang.org/x/term で入力を画面に出さずに読む。端末でなければ止める (pipe の値を黙って受けない)。
// 入力中の Ctrl-C でも端末の echo を戻してから終わる。
func (p terminalPrompter) Hidden(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("値の入力には端末が要る (stdin が端末でない)")
	}
	state, err := term.GetState(fd)
	if err != nil {
		return "", fmt.Errorf("端末の状態を読めない: %w", err)
	}
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	defer func() {
		signal.Stop(interrupted)
		close(done)
	}()
	go func() {
		select {
		case <-interrupted:
			_ = term.Restore(fd, state)
			_, _ = fmt.Fprintln(os.Stderr)
			os.Exit(130)
		case <-done:
		}
	}()
	_, _ = fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("入力を読めない: %w", err)
	}
	return string(value), nil
}
