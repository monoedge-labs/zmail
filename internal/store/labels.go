package store

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxLabelRunes is the longest a label may be.
const MaxLabelRunes = 40

// CleanLabel trims a label and refuses one that is empty, too long or spans
// lines. Labels are free text, told apart by their exact characters.
func CleanLabel(l string) (string, error) {
	l = strings.TrimSpace(l)
	switch {
	case l == "":
		return "", errors.New("a label cannot be empty")
	case utf8.RuneCountInString(l) > MaxLabelRunes:
		return "", fmt.Errorf("label %q is longer than %d characters", l, MaxLabelRunes)
	case strings.IndexFunc(l, unicode.IsControl) >= 0:
		return "", fmt.Errorf("label %q holds a control character", l)
	}
	return l, nil
}

// AddLabels puts every label on every message. A label a message already
// has stays as it is; a message that does not exist is an error and nothing
// is added.
func (s *Store) AddLabels(ids []int64, labels []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE id = ?`, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("message %d: not found", id)
		}
		for _, l := range labels {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO labels (message_id, label) VALUES (?,?)`, id, l); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// RemoveLabels takes every label off every message; one that is not there is
// not an error.
func (s *Store) RemoveLabels(ids []int64, labels []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		for _, l := range labels {
			if _, err := tx.Exec(`DELETE FROM labels WHERE message_id = ? AND label = ?`, id, l); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// Labels returns the labels of each of the messages, sorted; a message with
// none has no entry.
func (s *Store) Labels(ids []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	const batch = 500 // under SQLite's limit on variables
	for len(ids) > 0 {
		n := min(batch, len(ids))
		args := make([]any, n)
		for i, id := range ids[:n] {
			args[i] = id
		}
		rows, err := s.db.Query(`SELECT message_id, label FROM labels WHERE message_id IN (?`+strings.Repeat(",?", n-1)+`) ORDER BY label`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var l string
			if err := rows.Scan(&id, &l); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = append(out[id], l)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		ids = ids[n:]
	}
	return out, nil
}

// LabelCount is a label and how many messages carry it.
type LabelCount struct {
	Label string
	Count int
}

// LabelCounts lists every label in use, the most used first. Junk is left
// out, as it is from searches.
func (s *Store) LabelCounts() ([]LabelCount, error) {
	rows, err := s.db.Query(`SELECT l.label, COUNT(*) FROM labels l JOIN messages m ON m.id = l.message_id
		WHERE m.role != 'junk' GROUP BY l.label ORDER BY COUNT(*) DESC, l.label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LabelCount
	for rows.Next() {
		var c LabelCount
		if err := rows.Scan(&c.Label, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
