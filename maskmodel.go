package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/monoedge-labs/zmail/internal/guard"
	"github.com/monoedge-labs/zmail/internal/redact"
	"github.com/monoedge-labs/zmail/internal/store"
)

// findingsFormat versions how findings are kept on disk; a new one leaves the
// old ones unread.
const findingsFormat = "1"

// storedFindings is what the model found in text on an earlier run, if it did.
// rev names the model's files, so another model starts clean.
func storedFindings(st *store.Store, rev, text string) ([]redact.Found, bool) {
	if st == nil {
		return nil, false
	}
	b, ok := st.Findings(findingsKey(rev, text))
	if !ok {
		return nil, false
	}
	var found []redact.Found
	if json.Unmarshal(b, &found) != nil {
		return nil, false
	}
	return found, true
}

func findingsKey(rev, text string) []byte {
	k := sha256.Sum256([]byte(findingsFormat + "\x00" + rev + "\x00" + text))
	return k[:]
}

// storedModel has inner read a text once: what it finds is kept in st, and
// the next time, in this run or another, the model is not asked. A store that
// fails costs the saving, never the masking.
func storedModel(st *store.Store, rev string, inner redact.Model) redact.Model {
	if st == nil {
		return inner
	}
	return func(text string) ([]redact.Found, bool) {
		if found, ok := storedFindings(st, rev, text); ok {
			return found, true
		}
		found, ready := inner(text)
		if ready {
			if b, err := json.Marshal(found); err == nil {
				st.SetFindings(findingsKey(rev, text), b)
			}
		}
		return found, ready
	}
}

// viewModel reads the messages the window shows with the local model, off
// the main thread: the model takes a moment for a long message, and the
// window must not wait for it. A text it has not read yet is asked for here
// and read in the background; the window shows it once read.
// ponytail: the model stays loaded once read (about 40 MB); unload it after
// a quiet while if that matters.
type viewModel struct {
	dir      func() string // where the model's files are
	done     func()        // called when texts were read, to show them
	windowed func() bool   // false under test, where it is read at once
	st       *store.Store  // keeps what was found, so a text is read once, not at every launch

	mu      sync.Mutex
	model   *guard.Model
	err     string
	cache   map[[32]byte][]redact.Found
	waiting map[[32]byte]bool // queued or being read
	queue   []queued          // the last one is read first: the newest ask is what is on screen
	running bool
}

// queueMax is how many texts wait for the model: about what a window shows,
// a second or two of reading.
const queueMax = 64

type queued struct {
	k    [32]byte
	text string
}

// find is a redact.Model.
func (v *viewModel) find(text string) ([]redact.Found, bool) {
	k := sha256.Sum256([]byte(text))
	v.mu.Lock()
	if f, ok := v.cache[k]; ok {
		v.mu.Unlock()
		return f, true
	}
	if v.err != "" { // the model did not load: the window says so
		v.mu.Unlock()
		return nil, false
	}
	// A point query, quick enough to hold the lock: a text read in an earlier
	// run shows masked at once.
	if f, ok := storedFindings(v.st, filepath.Base(v.dir()), text); ok {
		v.remember(k, f)
		v.mu.Unlock()
		return f, true
	}
	if v.waiting == nil {
		v.waiting = map[[32]byte]bool{}
	}
	if !v.waiting[k] {
		v.waiting[k] = true
		v.queue = append(v.queue, queued{k, text})
		// What is asked for is what is in view. Asks left behind by a scroll
		// past them are dropped, oldest first: they are asked again when
		// they show.
		if len(v.queue) > queueMax {
			delete(v.waiting, v.queue[0].k)
			v.queue = v.queue[1:]
		}
	}
	start := !v.running
	v.running = true
	v.mu.Unlock()
	if start {
		if v.windowed != nil && v.windowed() {
			go v.work()
		} else {
			v.work()
			v.mu.Lock()
			defer v.mu.Unlock()
			f, ok := v.cache[k]
			return f, ok
		}
	}
	return nil, false
}

// work reads what is queued, and has the window show it as it goes, a few
// times a second, and when it is done.
func (v *viewModel) work() {
	var shown time.Time
	for {
		v.mu.Lock()
		if len(v.queue) == 0 {
			v.running = false
			v.mu.Unlock()
			if v.done != nil {
				v.done()
			}
			return
		}
		q := v.queue[len(v.queue)-1]
		v.queue = v.queue[:len(v.queue)-1]
		m := v.model
		v.mu.Unlock()

		if m == nil {
			var err error
			if m, err = guard.Load(v.dir()); err != nil {
				v.mu.Lock()
				v.err, v.queue, v.waiting = fmt.Sprintf("本地识别模型没有加载：%v", err), nil, nil
				v.mu.Unlock()
				continue
			}
		}
		found, _ := storedModel(v.st, filepath.Base(v.dir()), redact.FromGuard(m))(q.text)
		v.mu.Lock()
		v.model = m
		v.remember(q.k, found)
		delete(v.waiting, q.k)
		v.mu.Unlock()
		if v.done != nil && time.Since(shown) > 500*time.Millisecond {
			shown = time.Now()
			v.done()
		}
	}
}

// remember keeps what was found in memory, with v.mu held.
func (v *viewModel) remember(k [32]byte, found []redact.Found) {
	if v.cache == nil || len(v.cache) > 2000 {
		v.cache = map[[32]byte][]redact.Found{}
	}
	v.cache[k] = found
}

// failure is why the model could not be read, or "".
func (v *viewModel) failure() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.err
}

// reset forgets a failure, so the next reading loads the model again.
func (v *viewModel) reset() {
	v.mu.Lock()
	v.err = ""
	v.mu.Unlock()
}

// download is the model's download, as the statistics page shows it.
type download struct {
	busy        bool
	done, total int64
	err         string
}

// ensureModel starts the model's download when something is to be hidden
// with it and it is not here.
func (a *app) ensureModel() {
	if (a.priv.View || a.priv.Agent) && a.priv.model() && a.dir != "" && !guard.Installed(a.modelFiles()) {
		a.downloadModel()
	}
}

// downloadModel fetches the local model's files, a few times a second telling the window how far.
func (a *app) downloadModel() {
	if a.dl.busy || a.dir == "" || a.win == nil {
		return
	}
	a.dl = download{busy: true, total: guard.Size()}
	dir := modelDir(a.dir)
	finish := func(err error) {
		a.dl.busy = false
		if err != nil {
			a.dl.err = err.Error()
		}
		a.vm.reset()
		a.reload()
	}
	go func() {
		var last time.Time
		err := guard.Install(context.Background(), dir, func(done, total int64) {
			if time.Since(last) < 250*time.Millisecond && done < total {
				return
			}
			last = time.Now()
			a.win.Update(func() { a.dl.done, a.dl.total = done, total })
		})
		a.win.Update(func() { finish(err) })
	}()
}

// modelDir of the app, where its files are.
func (a *app) modelFiles() string { return modelDir(a.dir) }
