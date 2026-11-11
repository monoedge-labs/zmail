package guard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The model's files are fetched from Hugging Face at the revision whose
// sha256 are pinned here (the pins are magpie's, from the publisher's own
// listing). hf-mirror.com serves the same files where Hugging Face is out of
// reach; what either serves is checked against the pin.

const rev = "7216b6a3b897ef903d4b1ba367ae6fb95b0fa5e4" // QuantumNous/astrlink-guard v0.1.3

type pinned struct {
	path string // in the repo; saved under its base name
	size int64
	sha  string
}

var pins = []pinned{
	{"int8/model_int8.onnx", 330781, "0a070d3113615e33ba3b84d6bd486f663a377c04c4b056e7381732179f51832a"},
	{"int8/model_int8.onnx_data", 37124608, "0ba34216ceead01e0ae2dfd8e46d52b67e7aee80b04f722d9b758613a91c0067"},
	{"int8/tokenizer.json", 1502881, "0cf3b235ef125658015a6c8026a5fe6f28088bf825101a990ec18a10200feef4"},
}

var hosts = []string{"https://huggingface.co", "https://hf-mirror.com"}

// Dir is where the model lives under root: a folder per revision.
func Dir(root string) string { return filepath.Join(root, "astrlink-guard", rev[:12]) }

// Size is what Install downloads, in bytes.
func Size() (n int64) {
	for _, p := range pins {
		n += p.size
	}
	return n
}

// Installed says every file is in dir at its size.
func Installed(dir string) bool {
	for _, p := range pins {
		if fi, err := os.Stat(filepath.Join(dir, filepath.Base(p.path))); err != nil || fi.Size() != p.size {
			return false
		}
	}
	return true
}

// Install fetches the files dir lacks, calling progress with the bytes
// of all of them fetched so far and Size().
func Install(ctx context.Context, dir string, progress func(done, total int64)) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var base int64
	for _, p := range pins {
		path := filepath.Join(dir, filepath.Base(p.path))
		if fi, err := os.Stat(path); err != nil || fi.Size() != p.size {
			var err error
			for _, h := range hosts {
				if err = fetch(ctx, h+"/QuantumNous/astrlink-guard/resolve/"+rev+"/"+p.path, path, p, func(n int64) { progress(base+n, Size()) }); err == nil {
					break
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			if err != nil {
				return fmt.Errorf("downloading %s: %w", filepath.Base(p.path), err)
			}
		}
		base += p.size
		progress(base, Size())
	}
	return nil
}

// fetch saves url at path once its size and sha256 are the pin's.
func fetch(ctx context.Context, url, path string, p pinned, progress func(int64)) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, res.Status)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".part-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	var n int64
	buf := make([]byte, 64<<10)
	for {
		k, err := res.Body.Read(buf)
		if k > 0 {
			h.Write(buf[:k])
			if _, werr := f.Write(buf[:k]); werr != nil {
				f.Close()
				return werr
			}
			if n += int64(k); n > p.size {
				f.Close()
				return fmt.Errorf("%s: more than the %d bytes pinned", url, p.size)
			}
			progress(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if n != p.size || hex.EncodeToString(h.Sum(nil)) != p.sha {
		return fmt.Errorf("%s: not the pinned file", url)
	}
	return os.Rename(f.Name(), path)
}
