package store

import "context"

func (s *Store) SetBusyDay(ctx context.Context, memberID int64, day string, on bool) error {
	if on {
		_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO busy_days(member_id, day) VALUES (?,?)`, memberID, day)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM busy_days WHERE member_id=? AND day=?`, memberID, day)
	return err
}

// BusyDays of a member from a day on
func (s *Store) BusyDays(ctx context.Context, memberID int64, from string) []string {
	return s.strs(ctx, `SELECT day FROM busy_days WHERE member_id=? AND day>=? ORDER BY day`, memberID, from)
}

func (s *Store) SetFreeSlot(ctx context.Context, memberID int64, day string, part int, on bool) error {
	if on {
		_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO free_slots(member_id, day, part) VALUES (?,?,?)`, memberID, day, part)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM free_slots WHERE member_id=? AND day=? AND part=?`, memberID, day, part)
	return err
}

// FreeSlots of a member from a day on, as "YYYY-MM-DD:part"
func (s *Store) FreeSlots(ctx context.Context, memberID int64, from string) []string {
	return s.strs(ctx, `SELECT day || ':' || part FROM free_slots WHERE member_id=? AND day>=? ORDER BY day, part`, memberID, from)
}

func (s *Store) strs(ctx context.Context, q string, args ...any) []string {
	out := []string{}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if rows.Scan(&v) == nil {
			out = append(out, v)
		}
	}
	return out
}
