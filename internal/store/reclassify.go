package store

import (
	"encoding/json"
	"fmt"

	"github.com/blezek/lapdog/internal/classify"
	"github.com/blezek/lapdog/internal/sessionyaml"
)

// Reclassify replays every session's stored provenance through the current
// classifier and rewrites the classification columns where the answer changed.
//
// This is what makes an incorrect classification rule survivable: history is
// fixed in place with no re-driving. It is the intended remedy for the unverified
// AI detection field once a real AI session has confirmed the field name.
//
// Rows whose provenance is missing or unusable are skipped rather than failing the
// run, because a partial fix is better than none.
func (s *Store) Reclassify() (int, error) {
	rows, err := s.reader.Query(
		`SELECT id, session_num, session_type, event_context,
		        ai_opponent_count, COALESCE(ai_detection, ''), classify_source_json
		 FROM sessions ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("store: read sessions for reclassify: %w", err)
	}

	type pending struct {
		id      int64
		st, ctx string
		aiCount int
		aiHow   string
	}
	var work []pending

	for rows.Next() {
		var (
			id      int64
			num     int
			st, ctx string
			aiCount int
			aiHow   string
			source  string
		)
		if err := rows.Scan(&id, &num, &st, &ctx, &aiCount, &aiHow, &source); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: scan session for reclassify: %w", err)
		}
		if source == "" || source == "{}" {
			continue
		}
		var info sessionyaml.Info
		if err := json.Unmarshal([]byte(source), &info); err != nil {
			continue
		}
		res := classify.Classify(&info, num)
		if string(res.SessionType) == st &&
			string(res.EventContext) == ctx &&
			res.AIOpponentCount == aiCount &&
			string(res.AIDetection) == aiHow {
			continue
		}
		work = append(work, pending{
			id:      id,
			st:      string(res.SessionType),
			ctx:     string(res.EventContext),
			aiCount: res.AIOpponentCount,
			aiHow:   string(res.AIDetection),
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("store: iterate sessions for reclassify: %w", err)
	}
	rows.Close()

	if len(work) == 0 {
		return 0, nil
	}

	tx, err := s.writer.Begin()
	if err != nil {
		return 0, fmt.Errorf("store: begin reclassify: %w", err)
	}
	for _, p := range work {
		if _, err := tx.Exec(
			`UPDATE sessions
			 SET session_type = ?, event_context = ?, ai_opponent_count = ?,
			     ai_detection = ?, updated_at = ?
			 WHERE id = ?`,
			p.st, p.ctx, p.aiCount, p.aiHow, Now(), p.id,
		); err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("store: reclassify session %d: %w", p.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit reclassify: %w", err)
	}
	return len(work), nil
}
