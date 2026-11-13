package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The window installs the command line tool as a link: /usr/local/bin/zmail,
// which the default PATH holds, points to the executable inside zmail.app.
// The app updates itself by replacing zmail.app in place, so the link follows
// the app's version and needs no update of its own.

// cliLink is where the link is made. A variable, so that tests use a
// directory of their own and never touch the real one.
var cliLink = "/usr/local/bin/zmail"

// cliExecutable is the program that is running, links resolved: what the link
// points to. A variable for tests.
var cliExecutable = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// cliElevate makes the link with the user's administrator rights, for a
// directory the user cannot write. A variable, so that tests never open the
// system's authorization dialog.
var cliElevate = elevatedLink

// errCLICanceled is the user closing the authorization dialog: no error to
// show, only nothing done.
var errCLICanceled = errors.New("canceled")

// cliState is what the link's path holds.
type cliState int

const (
	cliAbsent  cliState = iota // nothing: it can be installed
	cliCurrent                 // a link to this app
	cliOther                   // something else, which an update replaces
)

// cliStatus is the state of the link's path, and for cliOther what is there.
type cliStatus struct {
	State cliState
	What  string // "指向 /opt/homebrew/bin/zmail", "指向 … （已失效）", "一个普通文件"
}

// cliStatusOf reads the state of link against the executable exe that should
// be behind it.
func cliStatusOf(link, exe string) cliStatus {
	fi, err := os.Lstat(link)
	switch {
	case err != nil: // nothing there, or nothing that can be seen: installing finds out which
		return cliStatus{State: cliAbsent}
	case fi.IsDir():
		return cliStatus{State: cliOther, What: "一个文件夹"}
	case fi.Mode()&fs.ModeSymlink == 0:
		return cliStatus{State: cliOther, What: "一个普通文件"}
	}
	to, _ := os.Readlink(link)
	if !filepath.IsAbs(to) {
		to = filepath.Join(filepath.Dir(link), to)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		return cliStatus{State: cliOther, What: "指向 " + to + "（已失效）"}
	}
	want, err := filepath.EvalSymlinks(exe)
	if err != nil {
		want = exe
	}
	if resolved == want {
		return cliStatus{State: cliCurrent}
	}
	return cliStatus{State: cliOther, What: "指向 " + to}
}

// cliBlocked says whether the app runs from a place a link must not point to:
// a disk image, or the randomized read-only copy that macOS runs a downloaded
// app from until it is moved. Both are gone later, and the link with them.
func cliBlocked(exe string) bool {
	return strings.Contains(exe, "/AppTranslocation/") || strings.HasPrefix(exe, "/Volumes/")
}

// linkCLI makes link point to exe, replacing a link or file that is there. The
// user's own rights come first; a directory they cannot write goes to
// cliElevate. A folder at link is left alone.
func linkCLI(link, exe string) error {
	if fi, err := os.Lstat(link); err == nil && fi.IsDir() {
		return fmt.Errorf("%s 是一个文件夹，没有替换", link)
	}
	err := linkDirect(link, exe)
	if errors.Is(err, fs.ErrPermission) {
		return cliElevate(link, exe)
	}
	return err
}

// linkDirect makes the link beside its final name and renames it over, so that
// the old one stays until the new one is in place.
func linkDirect(link, exe string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", link, os.Getpid())
	_ = os.Remove(tmp)
	if err := os.Symlink(exe, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// elevateScript runs the link command under the system's authorization
// dialog. The paths are never part of the script: they come in as arguments
// and are quoted for the shell by AppleScript itself.
const elevateScript = `on run argv
	set cmd to "/bin/mkdir -p " & quoted form of (item 1 of argv) & " && /bin/ln -sfn " & quoted form of (item 2 of argv) & " " & quoted form of (item 3 of argv)
	do shell script cmd with administrator privileges
end run`

// elevateArgs are the arguments of osascript for making link point to exe.
func elevateArgs(link, exe string) []string {
	return []string{"-e", elevateScript, filepath.Dir(link), exe, link}
}

// elevatedLink is cliElevate: the user's cancel of the dialog is AppleScript's
// error -128.
func elevatedLink(link, exe string) error {
	out, err := exec.Command("/usr/bin/osascript", elevateArgs(link, exe)...).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if strings.Contains(msg, "(-128)") {
		return errCLICanceled
	}
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("没有装上：%s", msg)
}

// cliPanel is what the statistics page's 命令行工具 panel shows. The disk is
// read when the page opens and after an action, never in a frame.
type cliPanel struct {
	read bool // st and why are read; navigate clears it, so that the page reads them again
	st   cliStatus
	exe  string // what a link would point to
	why  string // why it cannot be installed from here, "" when it can
	busy bool   // the link is being made, perhaps behind the authorization dialog
	err  string // what went wrong in the last action
}

// checkCLI reads the state of the link and of the app that runs.
func (a *app) checkCLI() {
	s := &a.cliTool
	s.read, s.why = true, ""
	exe, err := cliExecutable()
	switch {
	case err != nil:
		s.st, s.exe, s.why = cliStatus{}, "", "找不到 Zmail 的位置："+err.Error()
	case cliBlocked(exe):
		s.exe, s.why = exe, "Zmail 正在只读或临时的位置运行，请先把它移到“应用程序”文件夹再安装"
		s.st = cliStatusOf(cliLink, exe)
	default:
		s.exe = exe
		s.st = cliStatusOf(cliLink, exe)
	}
}

// installCLI makes the link to this app, off the main thread: the
// authorization dialog waits for the user. The panel shows the result after.
func (a *app) installCLI() {
	s := &a.cliTool
	if s.busy || s.why != "" {
		return
	}
	s.busy, s.err = true, ""
	link, exe := cliLink, s.exe
	done := func(err error) {
		s.busy = false
		if err != nil && !errors.Is(err, errCLICanceled) {
			s.err = err.Error()
		}
		a.checkCLI()
	}
	if a.win == nil { // under test: no window, no other thread
		done(linkCLI(link, exe))
		return
	}
	go func() {
		err := linkCLI(link, exe)
		a.win.Update(func() { done(err) })
	}()
}

// cliVersion is `zmail version`.
func cliVersion(args []string, out io.Writer) error {
	const usage = "zmail version [--json]"
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print JSON")
	words, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(words) > 0 {
		return fmt.Errorf("unexpected positional argument %q: usage: %s", words[0], usage)
	}
	if *asJSON {
		return json.NewEncoder(out).Encode(map[string]string{"version": appVersion()})
	}
	_, err = fmt.Fprintln(out, "zmail", appVersion())
	return err
}
