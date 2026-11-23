package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Thiritin/m3u-downloader/internal/catalog"
	"github.com/Thiritin/m3u-downloader/internal/store"
	"github.com/Thiritin/m3u-downloader/internal/xtream"
)

//go:embed frontend/*
var frontendFiles embed.FS

type Server struct {
	store      *store.Store
	xtream     *xtream.Client
	enqueueCfg catalog.EnqueueConfig
}

func New(st *store.Store, xc *xtream.Client, cfg catalog.EnqueueConfig) *Server {
	return &Server{
		store:      st,
		xtream:     xc,
		enqueueCfg: cfg,
	}
}

func (s *Server) ListenAndServe(addr string) error {
	frontend, err := fs.Sub(frontendFiles, "frontend")
	if err != nil {
		return fmt.Errorf("load frontend: %w", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/dashboard", s.handleDashboard)
	mux.HandleFunc("GET /api/categories", s.handleCategories)
	mux.HandleFunc("GET /api/catalog", s.handleCatalog)
	mux.HandleFunc("GET /api/movie", s.handleMovie)
	mux.HandleFunc("GET /api/series", s.handleSeries)
	mux.HandleFunc("GET /api/episodes", s.handleEpisodes)
	mux.HandleFunc("GET /api/queue/movie", s.handleMovieQueueStatus)
	mux.HandleFunc("POST /api/queue/movie", s.handleQueueMovie)
	mux.HandleFunc("DELETE /api/queue/movie", s.handleRemoveMovieFromQueue)

	mux.Handle("GET /", http.FileServer(http.FS(frontend)))

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Weboberfläche erreichbar unter http://%s", addr)

	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	kind := strings.ToLower(r.URL.Query().Get("type"))
	if kind != "vod" && kind != "series" {
		http.Error(w, "type must be vod or series", http.StatusBadRequest)
		return
	}

	categories, err := s.store.ListCategories(ctx, kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, categories)
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	query := r.URL.Query()

	kind := strings.ToLower(query.Get("type"))
	if kind != "vod" && kind != "series" {
		http.Error(w, "type must be vod or series", http.StatusBadRequest)
		return
	}

	categoryID, err := strconv.Atoi(query.Get("category_id"))
	if err != nil && query.Get("category_id") != "" {
		http.Error(w, "invalid category_id", http.StatusBadRequest)
		return
	}

	page, _ := strconv.Atoi(query.Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}

	offset := (page - 1) * limit
	search := strings.TrimSpace(query.Get("q"))

	var items any
	var total int

	switch kind {
	case "vod":
		result, count, err := s.store.ListVODPage(
			ctx, categoryID, search, limit, offset,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = result
		total = count

	case "series":
		result, count, err := s.store.ListSeriesPage(
			ctx, categoryID, search, limit, offset,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = result
		total = count
	}

	writeJSON(w, map[string]any{
		"type":       kind,
		"items":      items,
		"total":      total,
		"page":       page,
		"limit":      limit,
		"totalPages": (total + limit - 1) / limit,
	})
}

func (s *Server) handleMovie(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "invalid movie id", http.StatusBadRequest)
		return
	}

	movie, err := s.store.GetVOD(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if movie == nil {
		http.Error(w, "movie not found", http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]any{
		"movie": movie,
	})
}

func (s *Server) handleQueueMovie(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Request-Body begrenzen.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var request struct {
		ID int `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if request.ID <= 0 {
		http.Error(w, "invalid movie id", http.StatusBadRequest)
		return
	}

	// Film aus dem lokalen Katalog laden.
	movie, err := s.store.GetVOD(ctx, request.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if movie == nil {
		http.Error(w, "movie not found", http.StatusNotFound)
		return
	}

	// Bestehende Kataloglogik für Zielpfad und Warteschlange verwenden.
	err = catalog.EnqueueVOD(ctx, s.store, s.enqueueCfg, *movie)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyQueued) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{
				"status":  "already_queued",
				"message": "Dieser Film befindet sich bereits in der Warteschlange.",
			})
			return
		}

		log.Printf("queue movie %d failed: %v", request.ID, err)
		http.Error(w, "movie could not be queued", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{
		"status":  "queued",
		"message": "Film wurde zur Warteschlange hinzugefügt.",
	})
}

// handleMovieQueueStatus returns the current queue status of a movie.
func (s *Server) handleMovieQueueStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "invalid movie id", http.StatusBadRequest)
		return
	}

	status, err := s.store.GetQueuedJobStatus(ctx, "vod", id)
	if err != nil {
		log.Printf("get movie queue status %d failed: %v", id, err)
		http.Error(w, "queue status could not be loaded", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"queued": status != "",
		"status": status,
	})
}

// handleRemoveMovieFromQueue cancels a pending or active movie download.
func (s *Server) handleRemoveMovieFromQueue(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "invalid movie id", http.StatusBadRequest)
		return
	}

	status, err := s.store.RemoveQueuedJobBySource(ctx, "vod", id)
	if err != nil {
		log.Printf("remove movie %d from queue failed: %v", id, err)
		http.Error(w, "movie could not be removed from queue", http.StatusInternalServerError)
		return
	}

	if status == "" {
		writeJSON(w, map[string]any{
			"status":  "not_queued",
			"queued":  false,
			"message": "Dieser Film befindet sich nicht mehr in der Warteschlange.",
		})
		return
	}

	message := "Film wurde aus der Warteschlange entfernt."
	if status == "active" {
		message = "Der Abbruch des laufenden Downloads wurde angefordert."
	}

	writeJSON(w, map[string]any{
		"status":  "cancelled",
		"queued":  false,
		"message": message,
	})
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	// Der erste Abruf kann eine externe API-Anfrage auslösen.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "invalid series id", http.StatusBadRequest)
		return
	}

	series, err := s.store.GetSeries(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if series == nil {
		http.Error(w, "series not found", http.StatusNotFound)
		return
	}

	fetched, err := s.store.SeriesDetailsFetched(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Noch nicht geladen: Details beim Xtream-Anbieter abrufen.
	if !fetched {
		if s.xtream == nil {
			http.Error(w, "Xtream client not configured", http.StatusInternalServerError)
			return
		}

		info, err := s.xtream.GetSeriesInfo(ctx, id)
		if err != nil {
			log.Printf("get_series_info for series %d failed: %v", id, err)
			http.Error(w, "series details could not be fetched", http.StatusBadGateway)
			return
		}

		seasons := make([]store.SeasonRow, 0, len(info.Seasons))

		for _, season := range info.Seasons {
			seasons = append(seasons, store.SeasonRow{
				SeriesID:     id,
				SeasonNumber: season.SeasonNumber,
				Name:         season.Name,
				Overview:     season.Overview,
				CoverURL:     season.Cover,
			})
		}

		episodes := make([]store.EpisodeRow, 0)

		for seasonKey, seasonEpisodes := range info.Episodes {
			seasonNumber, err := strconv.Atoi(seasonKey)
			if err != nil {
				http.Error(w, "invalid season number in provider response", http.StatusBadGateway)
				return
			}

			for _, episode := range seasonEpisodes {
				episodeID, err := strconv.Atoi(episode.ID)
				if err != nil || episodeID <= 0 {
					http.Error(w, "invalid episode id in provider response", http.StatusBadGateway)
					return
				}

				episodes = append(episodes, store.EpisodeRow{
					EpisodeID:    episodeID,
					SeriesID:     id,
					SeasonNumber: seasonNumber,
					EpisodeNum:   episode.EpisodeNum,
					Title:        episode.Title,
					Plot:         episode.Info.Plot,
					ContainerExt: episode.ContainerExtension,
					DurationSecs: parseDurationSeconds(episode.Info.Duration),
				})
			}
		}

		// Erst Daten speichern, dann den Abruf als erfolgreich markieren.
		if err := s.store.ReplaceSeasonsAndEpisodes(ctx, id, seasons, episodes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := s.store.MarkSeriesDetailsFetched(ctx, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf(
			"series details cached: %s (ID %d, %d seasons, %d episodes)",
			series.Name, id, len(seasons), len(episodes),
		)
	}

	// Ab hier kommen die Daten aus SQLite.
	seasons, err := s.store.ListSeasons(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"series":  series,
		"seasons": seasons,
	})
}

// parseDurationSeconds converts provider durations such as "2700",
// "00:45:00" or "01:30:15" into seconds.
func parseDurationSeconds(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return seconds
	}

	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}

	values := make([]int, len(parts))
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return 0
		}
		values[i] = n
	}

	if len(values) == 2 {
		return values[0]*60 + values[1]
	}

	return values[0]*3600 + values[1]*60 + values[2]
}

func (s *Server) handleEpisodes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	query := r.URL.Query()

	seriesID, err := strconv.Atoi(query.Get("series_id"))
	if err != nil || seriesID <= 0 {
		http.Error(w, "invalid series_id", http.StatusBadRequest)
		return
	}

	season, err := strconv.Atoi(query.Get("season"))
	if err != nil || season < 0 {
		http.Error(w, "invalid season", http.StatusBadRequest)
		return
	}

	episodes, err := s.store.ListEpisodes(ctx, seriesID, season)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"episodes": episodes,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := s.store.DB().PingContext(ctx); err != nil {
		http.Error(w, "Datenbank nicht erreichbar", http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, map[string]any{
		"status":   "ok",
		"database": "connected",
	})
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	db := s.store.DB()

	var vodCount, seriesCount, categoryCount int64

	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM vods").Scan(&vodCount); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM series").Scan(&seriesCount); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM categories").Scan(&categoryCount); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rows, err := db.QueryContext(ctx, `
		SELECT status, COUNT(*)
		FROM jobs
		GROUP BY status
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	jobCounts := map[string]int64{
		"pending":   0,
		"active":    0,
		"completed": 0,
		"failed":    0,
		"cancelled": 0,
	}

	for rows.Next() {
		var status string
		var count int64

		if err := rows.Scan(&status, &count); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		jobCounts[status] = count
	}

	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"catalog": map[string]int64{
			"movies":     vodCount,
			"series":     seriesCount,
			"categories": categoryCount,
		},
		"jobs": jobCounts,
	})
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("JSON response error: %v", err)
	}
}
