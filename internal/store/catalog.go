package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CountAll returns the total number of cached vods and series.
// Used to decide whether the TUI should kick off an initial sync.
func (s *Store) CountAll(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM vods) + (SELECT count(*) FROM series)`).Scan(&n)
	return n, err
}

// ListAllVODs returns every cached VOD (used by global search).
func (s *Store) ListAllVODs(ctx context.Context) ([]VODRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT stream_id,category_id,name,COALESCE(year,0),COALESCE(plot,''),
		        COALESCE(stream_icon_url,''),COALESCE(container_extension,''),
		        COALESCE(added,0),COALESCE(rating,0)
		 FROM vods ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VODRow
	for rows.Next() {
		var v VODRow
		if err := rows.Scan(&v.StreamID, &v.CategoryID, &v.Name, &v.Year, &v.Plot,
			&v.StreamIcon, &v.ContainerExt, &v.Added, &v.Rating); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetVOD returns a single cached VOD by streamID,
// or (nil, nil) if it does not exist.
func (s *Store) GetVOD(ctx context.Context, streamID int) (*VODRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT stream_id, category_id, name,
		       COALESCE(year,0), COALESCE(plot,''),
		       COALESCE(stream_icon_url,''),
		       COALESCE(container_extension,''),
		       COALESCE(added,0), COALESCE(rating,0)
		FROM vods
		WHERE stream_id=?`, streamID)

	var v VODRow
	if err := row.Scan(
		&v.StreamID,
		&v.CategoryID,
		&v.Name,
		&v.Year,
		&v.Plot,
		&v.StreamIcon,
		&v.ContainerExt,
		&v.Added,
		&v.Rating,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &v, nil
}

// ListAllSeries returns every cached series (used by global search).
// GetSeries returns the series row for seriesID, or (nil, nil) if not found.
func (s *Store) GetSeries(ctx context.Context, seriesID int) (*SeriesRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT series_id,category_id,name,COALESCE(year,0),COALESCE(plot,''),
		        COALESCE(cover_url,''),COALESCE(backdrop_url,'')
		 FROM series WHERE series_id=?`, seriesID)
	var r SeriesRow
	if err := row.Scan(&r.SeriesID, &r.CategoryID, &r.Name, &r.Year, &r.Plot,
		&r.CoverURL, &r.BackdropURL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (s *Store) GetEpisode(ctx context.Context, episodeID int) (*EpisodeRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT episode_id,series_id,season_number,episode_num,
		       COALESCE(title,''),COALESCE(plot,''),COALESCE(container_extension,''),
		       COALESCE(duration_secs,0)
		FROM episodes WHERE episode_id=?`, episodeID)
	var e EpisodeRow
	err := row.Scan(
		&e.EpisodeID, &e.SeriesID, &e.SeasonNumber, &e.EpisodeNum,
		&e.Title, &e.Plot, &e.ContainerExt, &e.DurationSecs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) ListAllSeries(ctx context.Context) ([]SeriesRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT series_id,category_id,name,COALESCE(year,0),COALESCE(plot,''),
		        COALESCE(cover_url,''),COALESCE(backdrop_url,'')
		 FROM series ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeriesRow
	for rows.Next() {
		var r SeriesRow
		if err := rows.Scan(&r.SeriesID, &r.CategoryID, &r.Name, &r.Year, &r.Plot,
			&r.CoverURL, &r.BackdropURL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type CategoryRow struct {
	ID        int
	Type      string
	Name      string
	ParentID  int
	FetchedAt int64
}

type VODRow struct {
	StreamID     int
	CategoryID   int
	Name         string
	Year         int
	Plot         string
	StreamIcon   string
	ContainerExt string
	Added        int64
	Rating       float64
}

type SeriesRow struct {
	SeriesID    int
	CategoryID  int
	Name        string
	Year        int
	Plot        string
	CoverURL    string
	BackdropURL string
}

type SeasonRow struct {
	SeriesID     int
	SeasonNumber int
	Name         string
	Overview     string
	CoverURL     string
}

type EpisodeRow struct {
	EpisodeID    int
	SeriesID     int
	SeasonNumber int
	EpisodeNum   int
	Title        string
	Plot         string
	ContainerExt string
	DurationSecs int
}

func (s *Store) UpsertCategories(ctx context.Context, rows []CategoryRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO categories(id,type,name,parent_id) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, parent_id=excluded.parent_id, type=excluded.type`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx, r.ID, r.Type, r.Name, r.ParentID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListCategories(ctx context.Context, kind string) ([]CategoryRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, name, COALESCE(parent_id,0), COALESCE(fetched_at,0)
		 FROM categories WHERE type=? ORDER BY name`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CategoryRow
	for rows.Next() {
		var c CategoryRow
		if err := rows.Scan(&c.ID, &c.Type, &c.Name, &c.ParentID, &c.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ListCategoriesFiltered(ctx context.Context, kind string, hideAdult bool) ([]CategoryRow, error) {
	if !hideAdult {
		return s.ListCategories(ctx, kind)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, name, COALESCE(parent_id,0), COALESCE(fetched_at,0)
		 FROM categories
		 WHERE type=? AND name NOT LIKE 'FOR ADULTS%' COLLATE NOCASE
		 ORDER BY name`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CategoryRow
	for rows.Next() {
		var c CategoryRow
		if err := rows.Scan(&c.ID, &c.Type, &c.Name, &c.ParentID, &c.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) MarkCategoryFetched(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE categories SET fetched_at=? WHERE id=?`,
		time.Now().Unix(), id)
	return err
}

func (s *Store) UpsertVODs(ctx context.Context, rows []VODRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vods(stream_id,category_id,name,year,plot,stream_icon_url,container_extension,added,rating,fetched_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(stream_id) DO UPDATE SET
		  category_id=excluded.category_id, name=excluded.name, year=excluded.year, plot=excluded.plot,
		  stream_icon_url=excluded.stream_icon_url, container_extension=excluded.container_extension,
		  added=excluded.added, rating=excluded.rating, fetched_at=excluded.fetched_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx, r.StreamID, r.CategoryID, r.Name, r.Year, r.Plot,
			r.StreamIcon, r.ContainerExt, r.Added, r.Rating, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListVODs(ctx context.Context, categoryID int) ([]VODRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT stream_id,category_id,name,COALESCE(year,0),COALESCE(plot,''),
		        COALESCE(stream_icon_url,''),COALESCE(container_extension,''),
		        COALESCE(added,0),COALESCE(rating,0)
		 FROM vods WHERE category_id=? ORDER BY name`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VODRow
	for rows.Next() {
		var v VODRow
		if err := rows.Scan(&v.StreamID, &v.CategoryID, &v.Name, &v.Year, &v.Plot,
			&v.StreamIcon, &v.ContainerExt, &v.Added, &v.Rating); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertSeries(ctx context.Context, rows []SeriesRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO series(series_id,category_id,name,year,plot,cover_url,backdrop_url,fetched_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(series_id) DO UPDATE SET
		  category_id=excluded.category_id, name=excluded.name, year=excluded.year, plot=excluded.plot,
		  cover_url=excluded.cover_url, backdrop_url=excluded.backdrop_url, fetched_at=excluded.fetched_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx, r.SeriesID, r.CategoryID, r.Name, r.Year, r.Plot,
			r.CoverURL, r.BackdropURL, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListSeries(ctx context.Context, categoryID int) ([]SeriesRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT series_id,category_id,name,COALESCE(year,0),COALESCE(plot,''),
		        COALESCE(cover_url,''),COALESCE(backdrop_url,'')
		 FROM series WHERE category_id=? ORDER BY name`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeriesRow
	for rows.Next() {
		var r SeriesRow
		if err := rows.Scan(&r.SeriesID, &r.CategoryID, &r.Name, &r.Year, &r.Plot,
			&r.CoverURL, &r.BackdropURL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ReplaceSeasonsAndEpisodes(ctx context.Context, seriesID int, seasons []SeasonRow, episodes []EpisodeRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM seasons WHERE series_id=?`, seriesID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM episodes WHERE series_id=?`, seriesID); err != nil {
		return err
	}
	for _, s := range seasons {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO seasons(series_id,season_number,name,overview,cover_url) VALUES(?,?,?,?,?)`,
			s.SeriesID, s.SeasonNumber, s.Name, s.Overview, s.CoverURL); err != nil {
			return err
		}
	}
	for _, e := range episodes {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO episodes(episode_id,series_id,season_number,episode_num,title,plot,container_extension,duration_secs)
			 VALUES(?,?,?,?,?,?,?,?)`,
			e.EpisodeID, e.SeriesID, e.SeasonNumber, e.EpisodeNum, e.Title, e.Plot, e.ContainerExt, e.DurationSecs); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListSeasons(ctx context.Context, seriesID int) ([]SeasonRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT series_id,season_number,COALESCE(name,''),COALESCE(overview,''),COALESCE(cover_url,'')
		 FROM seasons WHERE series_id=? ORDER BY season_number`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeasonRow
	for rows.Next() {
		var s SeasonRow
		if err := rows.Scan(&s.SeriesID, &s.SeasonNumber, &s.Name, &s.Overview, &s.CoverURL); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *Store) ListEpisodes(ctx context.Context, seriesID, season int) ([]EpisodeRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT episode_id,series_id,season_number,episode_num,COALESCE(title,''),COALESCE(plot,''),
		        COALESCE(container_extension,''),COALESCE(duration_secs,0)
		 FROM episodes WHERE series_id=? AND season_number=? ORDER BY episode_num`, seriesID, season)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EpisodeRow
	for rows.Next() {
		var e EpisodeRow
		if err := rows.Scan(&e.EpisodeID, &e.SeriesID, &e.SeasonNumber, &e.EpisodeNum, &e.Title,
			&e.Plot, &e.ContainerExt, &e.DurationSecs); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListVODPage returns a filtered page of movies and the total match count.
func (s *Store) ListVODPage(ctx context.Context, categoryID int, search string, limit, offset int) ([]VODRow, int, error) {
	return s.ListVODPageFiltered(ctx, categoryID, search, limit, offset, false)
}

func vodSortSQL(sort string) string {
	switch sort {
	case "year_desc":
		return "ORDER BY COALESCE(year,0) DESC, name COLLATE NOCASE"
	case "year_asc":
		return "ORDER BY COALESCE(year,0) ASC, name COLLATE NOCASE"
	case "rating_desc":
		return "ORDER BY COALESCE(rating,0) DESC, name COLLATE NOCASE"
	case "name_desc":
		return "ORDER BY name COLLATE NOCASE DESC"
	default:
		return "ORDER BY name COLLATE NOCASE ASC"
	}
}

func seriesSortSQL(sort string) string {
	switch sort {
	case "year_desc":
		return "ORDER BY COALESCE(year,0) DESC, name COLLATE NOCASE"
	case "year_asc":
		return "ORDER BY COALESCE(year,0) ASC, name COLLATE NOCASE"
	case "name_desc":
		return "ORDER BY name COLLATE NOCASE DESC"
	default:
		return "ORDER BY name COLLATE NOCASE ASC"
	}
}

func (s *Store) ListVODPageFiltered(ctx context.Context, categoryID int, search string, limit, offset int, hideAdult bool) ([]VODRow, int, error) {
	return s.ListVODPageSorted(ctx, categoryID, search, limit, offset, hideAdult, "name")
}

func (s *Store) ListVODPageSorted(ctx context.Context, categoryID int, search string, limit, offset int, hideAdult bool, sort string) ([]VODRow, int, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	search = "%" + search + "%"

	var total int
	filterSQL := ""
	args := []any{categoryID, categoryID, search}
	if hideAdult {
		filterSQL = ` AND NOT EXISTS (
			SELECT 1 FROM categories c
			WHERE c.id = vods.category_id
			  AND c.name LIKE 'FOR ADULTS%' COLLATE NOCASE
		)`
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM vods
		WHERE (? = 0 OR category_id = ?)
		  AND name LIKE ? COLLATE NOCASE`+filterSQL,
		args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	orderBy := vodSortSQL(sort)
	rows, err := s.db.QueryContext(ctx, `
		SELECT stream_id, category_id, name,
		       COALESCE(year,0), COALESCE(plot,''),
		       COALESCE(stream_icon_url,''),
		       COALESCE(container_extension,''),
		       COALESCE(added,0), COALESCE(rating,0)
		FROM vods
		WHERE (? = 0 OR category_id = ?)
		  AND name LIKE ? COLLATE NOCASE`+filterSQL+`
		`+orderBy+`
		LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []VODRow
	for rows.Next() {
		var v VODRow
		if err := rows.Scan(
			&v.StreamID, &v.CategoryID, &v.Name,
			&v.Year, &v.Plot, &v.StreamIcon,
			&v.ContainerExt, &v.Added, &v.Rating,
		); err != nil {
			return nil, 0, err
		}
		result = append(result, v)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return result, total, nil
}

// ListSeriesPage returns a filtered page of series and the total match count.
func (s *Store) ListSeriesPage(ctx context.Context, categoryID int, search string, limit, offset int) ([]SeriesRow, int, error) {
	return s.ListSeriesPageFiltered(ctx, categoryID, search, limit, offset, false)
}

func (s *Store) ListSeriesPageFiltered(ctx context.Context, categoryID int, search string, limit, offset int, hideAdult bool) ([]SeriesRow, int, error) {
	return s.ListSeriesPageSorted(ctx, categoryID, search, limit, offset, hideAdult, "name")
}

func (s *Store) ListSeriesPageSorted(ctx context.Context, categoryID int, search string, limit, offset int, hideAdult bool, sort string) ([]SeriesRow, int, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	search = "%" + search + "%"

	var total int
	filterSQL := ""
	args := []any{categoryID, categoryID, search}
	if hideAdult {
		filterSQL = ` AND NOT EXISTS (
			SELECT 1 FROM categories c
			WHERE c.id = series.category_id
			  AND c.name LIKE 'FOR ADULTS%' COLLATE NOCASE
		)`
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM series
		WHERE (? = 0 OR category_id = ?)
		  AND name LIKE ? COLLATE NOCASE`+filterSQL,
		args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT series_id, category_id, name,
		       COALESCE(year,0), COALESCE(plot,''),
		       COALESCE(cover_url,''), COALESCE(backdrop_url,'')
		FROM series
		WHERE (? = 0 OR category_id = ?)
		  AND name LIKE ? COLLATE NOCASE`+filterSQL+`
		`+seriesSortSQL(sort)+`
		LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []SeriesRow
	for rows.Next() {
		var r SeriesRow
		if err := rows.Scan(
			&r.SeriesID, &r.CategoryID, &r.Name,
			&r.Year, &r.Plot, &r.CoverURL, &r.BackdropURL,
		); err != nil {
			return nil, 0, err
		}
		result = append(result, r)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return result, total, nil
}

// SeriesDetailsFetched reports whether get_series_info was successfully
// fetched and cached for the given series.
func (s *Store) SeriesDetailsFetched(ctx context.Context, seriesID int) (bool, error) {
	var fetched bool

	err := s.db.QueryRowContext(ctx, `
		SELECT details_fetched_at IS NOT NULL
		FROM series
		WHERE series_id = ?`, seriesID).Scan(&fetched)

	if err != nil {
		return false, err
	}

	return fetched, nil
}

// MarkSeriesDetailsFetched marks the series details as successfully cached.
func (s *Store) MarkSeriesDetailsFetched(ctx context.Context, seriesID int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE series
		SET details_fetched_at = unixepoch()
		WHERE series_id = ?`, seriesID)

	return err
}
