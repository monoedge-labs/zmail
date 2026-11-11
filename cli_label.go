package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

const labelUsage = "usage: zmail label add|remove --label L [--label L2] ID... [--json]  or  zmail label list [--json]"

type labelItem struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// cliLabel puts labels on messages, takes them off, and lists those in use.
// Labels are local to the database: the mail servers never see them.
func cliLabel(svc *mailbox.Service, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(labelUsage)
	}
	sub := args[0]
	fs := flag.NewFlagSet("label "+sub, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	var labels []string
	if sub == "add" || sub == "remove" {
		fs.Func("label", "a label; repeat the flag for more", func(v string) error { labels = append(labels, v); return nil })
	}
	if sub != "add" && sub != "remove" && sub != "list" {
		return fmt.Errorf("unknown label command %q: %s", sub, labelUsage)
	}
	words, err := parseFlags(fs, args[1:])
	if err != nil {
		return err
	}
	show := func(v any, text string) error {
		if *asJSON {
			return json.NewEncoder(out).Encode(v)
		}
		_, err := fmt.Fprintln(out, strings.TrimRight(text, "\n"))
		return err
	}

	if sub == "list" {
		if len(words) > 0 {
			return fmt.Errorf("unexpected positional argument %q: %s", words[0], labelUsage)
		}
		counts, err := svc.Store.LabelCounts()
		svc.Store.LogOp(store.Op{Actor: cliWho.Actor, Client: cliWho.Client, Tool: "list_labels", OK: err == nil})
		if err != nil {
			return err
		}
		items, lines := []labelItem{}, make([]string, 0, len(counts))
		for _, c := range counts {
			items = append(items, labelItem{c.Label, c.Count})
			lines = append(lines, fmt.Sprintf("%s  %d", c.Label, c.Count))
		}
		if len(lines) == 0 {
			lines = append(lines, "no labels")
		}
		return show(map[string]any{"labels": items}, strings.Join(lines, "\n"))
	}

	if len(labels) == 0 {
		return fmt.Errorf("label %s needs --label: %s", sub, labelUsage)
	}
	ids, err := parseIDs(words, false)
	if err != nil {
		return err
	}
	if err := svc.Label(cliWho, ids, labels, sub == "remove"); err != nil {
		return err
	}
	verb := "labeled"
	if sub == "remove" {
		verb = "unlabeled"
	}
	return show(map[string]int{"affected": len(ids)}, fmt.Sprintf("%s %d messages", verb, len(ids)))
}
