package pause

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidRule = errors.New("invalid pause rule")

type RuleInput struct {
	Scope           string `json:"scope"`
	Mode            string `json:"mode"`
	DurationMinutes int    `json:"duration_minutes"`
	StartAt         string `json:"start_at"`
	EndAt           string `json:"end_at"`
	Days            []int  `json:"days"`
	StartTime       string `json:"start_time"`
	EndTime         string `json:"end_time"`
}

type Rule struct {
	ID        int64      `json:"id"`
	Scope     string     `json:"scope"`
	Mode      string     `json:"mode"`
	StartsAt  *time.Time `json:"starts_at,omitempty"`
	EndsAt    *time.Time `json:"ends_at,omitempty"`
	Days      []int      `json:"days,omitempty"`
	StartTime string     `json:"start_time,omitempty"`
	EndTime   string     `json:"end_time,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type State struct {
	PausedDownloads bool   `json:"paused_downloads"`
	PausedIPTV      bool   `json:"paused_iptv"`
	DownloadUntil   string `json:"download_until,omitempty"`
	IPTVUntil       string `json:"iptv_until,omitempty"`
}

type Controller struct {
	db *sql.DB
}

func New(db *sql.DB) *Controller { return &Controller{db: db} }

func (c *Controller) Create(ctx context.Context, input RuleInput, now time.Time) (Rule, error) {
	input.Scope = strings.ToLower(strings.TrimSpace(input.Scope))
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Scope != "downloads" && input.Scope != "iptv" && input.Scope != "both" {
		return Rule{}, fmt.Errorf("%w: scope must be downloads, iptv or both", ErrInvalidRule)
	}
	if input.Mode != "indefinite" && input.Mode != "duration" && input.Mode != "at" && input.Mode != "schedule" {
		return Rule{}, fmt.Errorf("%w: unsupported mode", ErrInvalidRule)
	}

	var startsAt, endsAt any
	daysMask := 0
	startTime, endTime := "", ""
	switch input.Mode {
	case "indefinite":
		startsAt = now.Unix()
	case "duration":
		if input.DurationMinutes < 1 || input.DurationMinutes > 10080 {
			return Rule{}, fmt.Errorf("%w: duration must be between 1 minute and 7 days", ErrInvalidRule)
		}
		startsAt = now.Unix()
		endsAt = now.Add(time.Duration(input.DurationMinutes) * time.Minute).Unix()
	case "at":
		start, err := time.Parse(time.RFC3339, input.StartAt)
		if err != nil {
			return Rule{}, fmt.Errorf("%w: start_at must be RFC3339", ErrInvalidRule)
		}
		start = start.Local()
		if !start.After(now) {
			return Rule{}, fmt.Errorf("%w: start_at must be in the future", ErrInvalidRule)
		}
		startsAt = start.Unix()
		if input.EndAt != "" {
			end, err := time.Parse(time.RFC3339, input.EndAt)
			if err != nil {
				return Rule{}, fmt.Errorf("%w: end_at must be RFC3339", ErrInvalidRule)
			}
			end = end.Local()
			if !end.After(start) {
				return Rule{}, fmt.Errorf("%w: end_at must be after start_at", ErrInvalidRule)
			}
			endsAt = end.Unix()
		}
	case "schedule":
		if len(input.Days) == 0 {
			return Rule{}, fmt.Errorf("%w: choose at least one weekday", ErrInvalidRule)
		}
		if _, err := time.Parse("15:04", input.StartTime); err != nil {
			return Rule{}, fmt.Errorf("%w: invalid start_time", ErrInvalidRule)
		}
		if _, err := time.Parse("15:04", input.EndTime); err != nil || input.EndTime == input.StartTime {
			return Rule{}, fmt.Errorf("%w: invalid end_time", ErrInvalidRule)
		}
		for _, day := range input.Days {
			if day < 0 || day > 6 {
				return Rule{}, fmt.Errorf("%w: weekdays must be 0 through 6", ErrInvalidRule)
			}
			daysMask |= 1 << day
		}
		startTime, endTime = input.StartTime, input.EndTime
	}

	result, err := c.db.ExecContext(ctx, `
		INSERT INTO pause_rules(scope,mode,starts_at,ends_at,days_mask,start_time,end_time,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		input.Scope, input.Mode, startsAt, endsAt, daysMask, nullableString(startTime), nullableString(endTime), now.Unix())
	if err != nil {
		return Rule{}, fmt.Errorf("save pause rule: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Rule{}, fmt.Errorf("read pause rule ID: %w", err)
	}
	return c.get(ctx, id)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (c *Controller) get(ctx context.Context, id int64) (Rule, error) {
	rules, _, err := c.List(ctx, time.Now())
	if err != nil {
		return Rule{}, err
	}
	for _, rule := range rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return Rule{}, sql.ErrNoRows
}

func (c *Controller) List(ctx context.Context, now time.Time) ([]Rule, State, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT id,scope,mode,starts_at,ends_at,days_mask,
		       COALESCE(start_time,''),COALESCE(end_time,''),created_at
		FROM pause_rules ORDER BY created_at,id`)
	if err != nil {
		return nil, State{}, err
	}
	defer rows.Close()

	var rules []Rule
	state := State{}
	for rows.Next() {
		var rule Rule
		var startsAt, endsAt sql.NullInt64
		var daysMask int
		var createdAt int64
		if err := rows.Scan(&rule.ID, &rule.Scope, &rule.Mode, &startsAt, &endsAt,
			&daysMask, &rule.StartTime, &rule.EndTime, &createdAt); err != nil {
			return nil, State{}, err
		}
		rule.CreatedAt = time.Unix(createdAt, 0).Local()
		if startsAt.Valid {
			start := time.Unix(startsAt.Int64, 0).Local()
			rule.StartsAt = &start
		}
		if endsAt.Valid {
			end := time.Unix(endsAt.Int64, 0).Local()
			rule.EndsAt = &end
		}
		if rule.Mode == "schedule" {
			rule.Days = daysFromMask(daysMask)
		}
		rules = append(rules, rule)

		if isActive(rule, daysMask, now) {
			if rule.Scope == "downloads" || rule.Scope == "both" {
				state.PausedDownloads = true
				state.DownloadUntil = ruleEndDescription(rule)
			}
			if rule.Scope == "iptv" || rule.Scope == "both" {
				state.PausedIPTV = true
				state.IPTVUntil = ruleEndDescription(rule)
			}
		}
	}
	return rules, state, rows.Err()
}

func (c *Controller) IsPaused(ctx context.Context, scope string) (bool, error) {
	_, state, err := c.List(ctx, time.Now())
	if err != nil {
		return false, err
	}
	switch scope {
	case "downloads":
		return state.PausedDownloads, nil
	case "iptv":
		return state.PausedIPTV, nil
	default:
		return false, fmt.Errorf("unknown pause scope %q", scope)
	}
}

func (c *Controller) Wait(ctx context.Context, scope string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		paused, err := c.IsPaused(ctx, scope)
		if err != nil {
			return err
		}
		if !paused {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Controller) Delete(ctx context.Context, id int64) error {
	result, err := c.db.ExecContext(ctx, `DELETE FROM pause_rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func isActive(rule Rule, daysMask int, now time.Time) bool {
	switch rule.Mode {
	case "indefinite":
		return rule.StartsAt != nil && !now.Before(*rule.StartsAt)
	case "duration":
		return rule.StartsAt != nil && rule.EndsAt != nil &&
			!now.Before(*rule.StartsAt) && now.Before(*rule.EndsAt)
	case "at":
		return rule.StartsAt != nil && !now.Before(*rule.StartsAt) &&
			(rule.EndsAt == nil || now.Before(*rule.EndsAt))
	case "schedule":
		start, err1 := time.Parse("15:04", rule.StartTime)
		end, err2 := time.Parse("15:04", rule.EndTime)
		if err1 != nil || err2 != nil {
			return false
		}
		minute := now.Hour()*60 + now.Minute()
		startMinute := start.Hour()*60 + start.Minute()
		endMinute := end.Hour()*60 + end.Minute()
		if startMinute < endMinute {
			return daysMask&(1<<now.Weekday()) != 0 && minute >= startMinute && minute < endMinute
		}
		if minute >= startMinute {
			return daysMask&(1<<now.Weekday()) != 0
		}
		return minute < endMinute && daysMask&(1<<now.AddDate(0, 0, -1).Weekday()) != 0
	}
	return false
}

func daysFromMask(mask int) []int {
	var days []int
	for day := 0; day <= 6; day++ {
		if mask&(1<<day) != 0 {
			days = append(days, day)
		}
	}
	return days
}

func ruleEndDescription(rule Rule) string {
	if rule.EndsAt != nil {
		return rule.EndsAt.Format("02.01.2006 15:04")
	}
	if rule.Mode == "schedule" {
		return "Zeitplan"
	}
	return "bis manuell aufgehoben"
}

func ParseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("invalid pause rule ID")
	}
	return id, nil
}
