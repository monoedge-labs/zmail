package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// useCLIDir points the link at a directory of the test's own, and makes the
// authorization dialog fail the test: no test may touch /usr/local/bin or run
// osascript. A test of what happens behind the dialog sets cliElevate itself.
func useCLIDir(t *testing.T) {
	t.Helper()
	link, elevate, exe := cliLink, cliElevate, cliExecutable
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cliLink = filepath.Join(real, "bin", "zmail")
	cliElevate = func(link, exe string) error {
		t.Errorf("the authorization dialog was asked for %s -> %s", link, exe)
		return errors.New("no dialog in tests")
	}
	t.Cleanup(func() { cliLink, cliElevate, cliExecutable = link, elevate, exe })
}

// touch makes a file, and its directories.
func touch(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// readOnlyDir makes a directory that the user cannot write, which a test of
// the authorization path needs; root writes anywhere.
func readOnlyDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only directory")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	return dir
}

func TestCLIStatus(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	exe := touch(t, filepath.Join(root, "Zmail.app", "Contents", "MacOS", "zmail"))
	other := touch(t, filepath.Join(root, "opt", "zmail"))
	link := filepath.Join(root, "bin", "zmail")
	reset := func() { os.RemoveAll(filepath.Dir(link)); os.MkdirAll(filepath.Dir(link), 0o755) }

	os.RemoveAll(filepath.Dir(link))
	if st := cliStatusOf(link, exe); st.State != cliAbsent {
		t.Errorf("no directory: %+v", st)
	}
	reset()
	if st := cliStatusOf(link, exe); st.State != cliAbsent {
		t.Errorf("nothing there: %+v", st)
	}

	for _, c := range []struct {
		name  string
		make  func()
		state cliState
		what  string // part of the description, for cliOther
	}{
		{"link to this app", func() { os.Symlink(exe, link) }, cliCurrent, ""},
		{"relative link to this app", func() { os.Symlink("../Zmail.app/Contents/MacOS/zmail", link) }, cliCurrent, ""},
		{"link through another link", func() {
			os.Symlink(exe, filepath.Join(root, "hop"))
			os.Symlink(filepath.Join(root, "hop"), link)
		}, cliCurrent, ""},
		{"link elsewhere", func() { os.Symlink(other, link) }, cliOther, "指向 " + other},
		{"dead link", func() { os.Symlink(filepath.Join(root, "gone"), link) }, cliOther, "指向 " + filepath.Join(root, "gone") + "（已失效）"},
		{"plain file", func() { touch(t, link) }, cliOther, "一个普通文件"},
		{"folder", func() { os.Mkdir(link, 0o755) }, cliOther, "一个文件夹"},
	} {
		reset()
		os.Remove(filepath.Join(root, "hop"))
		c.make()
		st := cliStatusOf(link, exe)
		if st.State != c.state || st.What != c.what {
			t.Errorf("%s: got %+v, want state %d %q", c.name, st, c.state, c.what)
		}
	}
}

func TestLinkCLI(t *testing.T) {
	useCLIDir(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	exe := touch(t, filepath.Join(root, "Zmail.app", "zmail"))
	other := touch(t, filepath.Join(root, "other"))
	link := filepath.Join(root, "usr", "local", "bin", "zmail") // directories to make

	target := func() string { to, _ := os.Readlink(link); return to }
	if err := linkCLI(link, exe); err != nil || target() != exe {
		t.Fatalf("fresh install: %v, links to %q", err, target())
	}
	if err := linkCLI(link, exe); err != nil || target() != exe {
		t.Fatalf("install again: %v, links to %q", err, target())
	}
	os.Remove(link)
	os.Symlink(other, link)
	if err := linkCLI(link, exe); err != nil || target() != exe {
		t.Fatalf("replacing a link elsewhere: %v, links to %q", err, target())
	}
	os.Remove(link)
	touch(t, link)
	if err := linkCLI(link, exe); err != nil || target() != exe {
		t.Fatalf("replacing a file: %v, links to %q", err, target())
	}
	os.Remove(link)
	os.Symlink(filepath.Join(root, "gone"), link)
	if err := linkCLI(link, exe); err != nil || target() != exe {
		t.Fatalf("replacing a dead link: %v, links to %q", err, target())
	}
	if st := cliStatusOf(link, exe); st.State != cliCurrent {
		t.Errorf("after the install: %+v", st)
	}
	if ents, _ := os.ReadDir(filepath.Dir(link)); len(ents) != 1 {
		t.Errorf("the directory holds %d entries, want the link alone", len(ents))
	}

	// a folder is never replaced
	os.Remove(link)
	os.Mkdir(link, 0o755)
	keep := touch(t, filepath.Join(link, "keep"))
	if err := linkCLI(link, exe); err == nil || !strings.Contains(err.Error(), "文件夹") {
		t.Errorf("a folder at the path: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the folder lost its content: %v", err)
	}
}

// A directory the user cannot write goes to the authorization dialog, with the
// paths unchanged; the user's own cancel is no failure, and any other error
// does not.
func TestLinkCLIElevates(t *testing.T) {
	useCLIDir(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	exe := touch(t, filepath.Join(root, "Zmail.app", "zmail"))
	ro := readOnlyDir(t)

	var asked [][2]string
	var answer error
	cliElevate = func(link, exe string) error { asked = append(asked, [2]string{link, exe}); return answer }

	for _, link := range []string{
		filepath.Join(ro, "zmail"),               // the directory exists and is read-only
		filepath.Join(ro, "usr", "bin", "zmail"), // the directory cannot be made
	} {
		asked = nil
		if err := linkCLI(link, exe); err != nil {
			t.Errorf("%s: %v", link, err)
		}
		if len(asked) != 1 || asked[0] != [2]string{link, exe} {
			t.Errorf("%s: the dialog was asked %v", link, asked)
		}
		if _, err := os.Lstat(link); err == nil {
			t.Errorf("%s: made without the dialog", link)
		}
	}

	answer = errCLICanceled
	if err := linkCLI(filepath.Join(ro, "zmail"), exe); !errors.Is(err, errCLICanceled) {
		t.Errorf("canceled: %v", err)
	}

	// no permission is missing here: the parent is a file
	asked = nil
	file := touch(t, filepath.Join(root, "file"))
	if err := linkCLI(filepath.Join(file, "zmail"), exe); err == nil || len(asked) != 0 {
		t.Errorf("an error that is no permission's: %v, dialog asked %v", err, asked)
	}
}

// The paths reach AppleScript as arguments, and the script holds none of them,
// so that no name can become a command.
func TestElevateArgs(t *testing.T) {
	link := `/usr/local/it's a "bin"/$(touch pwned)/zmail`
	exe := "/Applications/My Zmail.app/Contents/MacOS/zmail'; rm -rf ~ #"
	args := elevateArgs(link, exe)
	if len(args) != 5 || args[0] != "-e" || args[1] != elevateScript {
		t.Fatalf("args = %q", args)
	}
	if args[2] != filepath.Dir(link) || args[3] != exe || args[4] != link {
		t.Errorf("paths = %q", args[2:])
	}
	for _, path := range args[2:] {
		if strings.Contains(args[1], path) {
			t.Errorf("the script holds %q", path)
		}
	}
	for _, want := range []string{"on run argv", "/bin/mkdir -p ", "/bin/ln -sfn ", "quoted form of (item 1 of argv)", "quoted form of (item 2 of argv)", "quoted form of (item 3 of argv)", "with administrator privileges"} {
		if !strings.Contains(elevateScript, want) {
			t.Errorf("the script lacks %q", want)
		}
	}
}

func TestCLIBlocked(t *testing.T) {
	for exe, want := range map[string]bool{
		"/Applications/Zmail.app/Contents/MacOS/zmail":                                        false,
		"/Users/me/Applications/Zmail.app/Contents/MacOS/zmail":                               false,
		"/Users/me/Downloads/Zmail.app/Contents/MacOS/zmail":                                  false,
		"/private/var/folders/ab/T/AppTranslocation/4F1E-77/d/Zmail.app/Contents/MacOS/zmail": true,
		"/Volumes/Zmail 0.5.2/Zmail.app/Contents/MacOS/zmail":                                 true,
		"/Volumes/Backup/Applications/Zmail.app/Contents/MacOS/zmail":                         true,
		"/Users/me/Volumes/Zmail.app/Contents/MacOS/zmail":                                    false,
	} {
		if got := cliBlocked(exe); got != want {
			t.Errorf("cliBlocked(%q) = %v, want %v", exe, got, want)
		}
	}
}

func TestVersionCLI(t *testing.T) {
	v := appVersion()
	if v == "" || v == "unknown" {
		t.Fatalf("the build has no version: %q", v)
	}
	for _, args := range [][]string{{"version"}, {"--version"}} {
		out, err := run(t, t.TempDir(), memSecrets{}, "", args...)
		if err != nil || out != "zmail "+v+"\n" {
			t.Errorf("%v printed %q, %v", args, out, err)
		}
		out, err = run(t, t.TempDir(), memSecrets{}, "", append(args, "--json")...)
		var got map[string]string
		if err != nil || json.Unmarshal([]byte(out), &got) != nil || len(got) != 1 || got["version"] != v {
			t.Errorf("%v --json printed %q, %v", args, out, err)
		}
	}
	if _, err := run(t, t.TempDir(), memSecrets{}, "", "version", "extra"); err == nil {
		t.Error("a stray word must be refused")
	}
	if _, err := run(t, t.TempDir(), memSecrets{}, "", "version", "--nope"); err == nil {
		t.Error("an unknown flag must be refused")
	}
	help, _ := run(t, t.TempDir(), memSecrets{}, "", "help")
	if !strings.Contains(help, "zmail version [--json]") {
		t.Error("help does not list version")
	}
	// main hands the subcommand to the CLI: the window does not open for it
	if !runCLI([]string{"version"}) || !runCLI([]string{"--version"}) {
		t.Error("runCLI does not take version")
	}
}

// The panel of the statistics page shows the three states of the link, and
// the button that installs it. Everything happens in directories of the test.
func TestCLIPanel(t *testing.T) {
	a := testApp(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	exe := touch(t, filepath.Join(root, "Zmail.app", "Contents", "MacOS", "zmail"))
	other := touch(t, filepath.Join(root, "opt", "zmail"))
	cliExecutable = func() (string, error) { return exe, nil }
	link := cliLink
	installed := "版本 " + appVersion() + " · 随应用更新自动保持最新"

	tt := ui.NewTester(a.view, 1360, 900)
	stats := func() { // enter the page again: it reads the link then
		t.Helper()
		for _, page := range []string{"收件箱", "统计"} {
			if err := tt.Click(page); err != nil {
				t.Fatal(err)
			}
			tt.Advance(600e6)
		}
	}
	stats()
	if !tt.HasText("命令行工具") || !tt.HasText("安装后可在终端使用 zmail") || tt.HasText("已安装") {
		t.Fatalf("not installed: %q", tt.Texts())
	}
	shot(t, tt, "15-cli-absent")

	// the state is read when the page opens, not in every frame
	os.MkdirAll(filepath.Dir(link), 0o755)
	os.Symlink(exe, link) // behind the window's back
	tt.Advance(100e6)
	if tt.HasText("已安装") {
		t.Fatal("the link was read in a frame")
	}
	os.RemoveAll(filepath.Dir(link))

	if err := tt.Click("安装"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if to, err := os.Readlink(link); err != nil || to != exe {
		t.Fatalf("the click made %q, %v", to, err)
	}
	for _, want := range []string{"已安装", link, installed} {
		if !tt.HasText(want) {
			t.Errorf("after the install: no %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("安装后可在终端使用 zmail") || a.cliTool.err != "" {
		t.Errorf("after the install: %q, error %q", tt.Texts(), a.cliTool.err)
	}
	shot(t, tt, "16-cli-installed")

	// something else at the path: where it points, and the update
	os.Remove(link)
	os.Symlink(other, link)
	stats()
	if !tt.HasText(link+" 已存在") || !tt.HasText("指向 "+other) || tt.HasText("已安装") {
		t.Fatalf("a link elsewhere: %q", tt.Texts())
	}
	shot(t, tt, "17-cli-other")
	if err := tt.Click("更新为本应用的 zmail"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if to, _ := os.Readlink(link); to != exe || !tt.HasText(installed) {
		t.Fatalf("after the update: links to %q, %q", to, tt.Texts())
	}

	// run from a disk image: no install, and a way out
	os.Remove(link)
	dmg := "/Volumes/Zmail/Zmail.app/Contents/MacOS/zmail"
	cliExecutable = func() (string, error) { return dmg, nil }
	stats()
	if !tt.HasText("请先把它移到“应用程序”文件夹再安装") || !tt.HasText("安装后可在终端使用 zmail") {
		t.Fatalf("from a disk image: %q", tt.Texts())
	}
	shot(t, tt, "18-cli-blocked")
	tt.Click("安装") // dimmed: the click must do nothing
	tt.Advance(300e6)
	if _, err := os.Lstat(link); err == nil || a.cliTool.busy {
		t.Errorf("a link was made from a disk image: %v", err)
	}
}

// What goes wrong shows in the panel, and the user's cancel of the dialog does not.
func TestCLIPanelErrors(t *testing.T) {
	a := testApp(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	exe := touch(t, filepath.Join(root, "Zmail.app", "zmail"))
	cliExecutable = func() (string, error) { return exe, nil }
	tt := ui.NewTester(a.view, 1360, 900)
	if err := tt.Click("统计"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(600e6)

	// the parent is a file: no permission to ask for, an error to show
	cliLink = filepath.Join(touch(t, filepath.Join(root, "file")), "zmail")
	a.cliTool.read = false
	tt.Frame()
	if err := tt.Click("安装"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if a.cliTool.err == "" || !tt.HasText(a.cliTool.err) || a.cliTool.busy {
		t.Fatalf("no error shown: %q, %q", a.cliTool.err, tt.Texts())
	}
	shot(t, tt, "19-cli-error")

	// a directory that wants the user's rights: the dialog, then the user cancels
	ro := readOnlyDir(t)
	cliLink = filepath.Join(ro, "zmail")
	var asked int
	cliElevate = func(link, exe string) error { asked++; return errCLICanceled }
	a.cliTool.read = false
	tt.Frame()
	if err := tt.Click("安装"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if asked != 1 || a.cliTool.err != "" || a.cliTool.busy || !tt.HasText("安装后可在终端使用 zmail") {
		t.Errorf("a canceled dialog: asked %d, error %q, busy %v", asked, a.cliTool.err, a.cliTool.busy)
	}
}
