package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/zalando/go-keyring"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

const keychainService = "zmail"

// keychain keeps the passwords of accounts in the system's keychain.
type keychain struct{}

func (keychain) Get(id string) (string, error) { return keyring.Get(keychainService, id) }
func (keychain) Set(id, s string) error        { return keyring.Set(keychainService, id, s) }
func (keychain) Delete(id string) error        { return keyring.Delete(keychainService, id) }

func main() {
	if runCLI(os.Args[1:]) {
		return
	}
	demo := flag.Bool("demo", false, "run on sample mail, in memory, touching no server")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "zmail: unknown command %q; see zmail help\n", flag.Arg(0))
		os.Exit(2)
	}

	dir, err := configDir()
	if err != nil {
		log.Fatal(err)
	}
	dbPath := filepath.Join(dir, "zmail.db")
	if *demo {
		dir, dbPath = os.TempDir(), ":memory:"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	var svc *mailbox.Service
	if *demo {
		svc = demoService(st)
	} else {
		svc = mailbox.New(st, keychain{})
	}
	a := newApp(st, svc)
	svc.Changed = a.refresh

	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Zmail", Width: 1360, Height: 900, MinWidth: 960, MinHeight: 640,
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHidden,
			// The window is the design's frosted sheet: the desktop shows
			// through it, blurred.
			Vibrancy: mygo.VibrancyUnderWindow,
			Content:  ui.View(a.view),
		})
		if !*demo {
			ctx, cancel := context.WithCancel(context.Background())
			a.win.OnClosed(cancel)
			go func() { // accounts added by `zmail account add` appear without a restart
				for t := time.NewTicker(5 * time.Second); ; {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						a.refresh()
					}
				}
			}()
			go svc.Run(ctx, 2*time.Minute, func(err error) {
				a.win.Update(func() { a.notice = err.Error() })
			})
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
