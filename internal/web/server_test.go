package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Thiritin/m3u-downloader/internal/catalog"
	"github.com/Thiritin/m3u-downloader/internal/store"
)

func TestJobHTTPActions(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	id, err := st.EnqueueJob(ctx, "vod", 42, "/movies/Test (2024)/Test (2024).mkv")
	if err != nil {
		t.Fatal(err)
	}

	server := New(st, nil, catalog.EnqueueConfig{})
	handler, err := server.Handler(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("test")},
	})
	if err != nil {
		t.Fatal(err)
	}

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/jobs status = %d", listResponse.Code)
	}
	if !strings.Contains(listResponse.Body.String(), `"status":"pending"`) {
		t.Fatalf("GET /api/jobs body = %s", listResponse.Body.String())
	}

	cancelResponse := httptest.NewRecorder()
	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/jobs/"+strconv.FormatInt(id, 10)+"/cancel", nil)
	cancelRequest.SetPathValue("id", strconv.FormatInt(id, 10))
	handler.ServeHTTP(cancelResponse, cancelRequest)
	if cancelResponse.Code != http.StatusOK {
		t.Fatalf("cancel status = %d: %s", cancelResponse.Code, cancelResponse.Body.String())
	}

	deleteResponse := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+strconv.FormatInt(id, 10), nil)
	deleteRequest.SetPathValue("id", strconv.FormatInt(id, 10))
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}

	failedID, err := st.EnqueueJob(ctx, "vod", 43, "/movies/Retry (2024)/Retry (2024).mkv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.FailJob(ctx, failedID, "test error"); err != nil {
		t.Fatal(err)
	}

	retryResponse := httptest.NewRecorder()
	retryRequest := httptest.NewRequest(http.MethodPost, "/api/jobs/"+strconv.FormatInt(failedID, 10)+"/retry", nil)
	retryRequest.SetPathValue("id", strconv.FormatInt(failedID, 10))
	handler.ServeHTTP(retryResponse, retryRequest)
	if retryResponse.Code != http.StatusOK {
		t.Fatalf("retry status = %d: %s", retryResponse.Code, retryResponse.Body.String())
	}

	cancelledID, err := st.EnqueueJob(ctx, "vod", 44, "/movies/Cancelled (2024)/Cancelled (2024).mkv")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CancelJob(ctx, cancelledID); err != nil {
		t.Fatal(err)
	}

	cancelledRetryResponse := httptest.NewRecorder()
	cancelledRetryRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/jobs/"+strconv.FormatInt(cancelledID, 10)+"/retry",
		nil,
	)
	cancelledRetryRequest.SetPathValue("id", strconv.FormatInt(cancelledID, 10))
	handler.ServeHTTP(cancelledRetryResponse, cancelledRetryRequest)
	if cancelledRetryResponse.Code != http.StatusOK {
		t.Fatalf("cancelled retry status = %d: %s",
			cancelledRetryResponse.Code,
			cancelledRetryResponse.Body.String())
	}
}

func TestReorderQueueHTTP(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	first, _ := st.EnqueueJob(ctx, "vod", 1, "/first")
	second, _ := st.EnqueueJob(ctx, "vod", 2, "/second")
	handler := New(st, nil, catalog.EnqueueConfig{})
	server, err := handler.Handler(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("ok")},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"job_ids":[%d,%d]}`, second, first)
	request := httptest.NewRequest(http.MethodPost, "/api/queue/reorder", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reorder status = %d: %s", response.Code, response.Body.String())
	}
	job, err := st.ClaimNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.ID != second {
		t.Fatalf("claimed job = %+v, want ID %d", job, second)
	}
}

func TestPauseRuleHTTP(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	handler, err := New(st, nil, catalog.EnqueueConfig{}).Handler(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("test")},
	})
	if err != nil {
		t.Fatal(err)
	}
	create := httptest.NewRequest(http.MethodPost, "/api/pauses",
		strings.NewReader(`{"scope":"downloads","mode":"duration","duration_minutes":15}`))
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", created.Code, created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"mode":"duration"`) {
		t.Fatalf("create response missing rule: %s", created.Body.String())
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/pauses", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"paused_downloads":true`) {
		t.Fatalf("GET /api/pauses = %d: %s", list.Code, list.Body.String())
	}
	var response struct {
		Rule struct {
			ID int64 `json:"id"`
		} `json:"rule"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	deleteRequest := httptest.NewRequest(http.MethodDelete,
		"/api/pauses/"+strconv.FormatInt(response.Rule.ID, 10), nil)
	deleteRequest.SetPathValue("id", strconv.FormatInt(response.Rule.ID, 10))
	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", deleted.Code, deleted.Body.String())
	}
}

func TestMovieCoverMissingMovie(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	server := New(st, nil, catalog.EnqueueConfig{})
	handler, err := server.Handler(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("test")},
	})
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/cover/movie?id=999999", nil),
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cover missing status = %d, want 404", response.Code)
	}
}

func TestEpisodeQueueHTTPActions(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	if err := st.UpsertSeries(ctx, []store.SeriesRow{{
		SeriesID:   100,
		CategoryID: 1,
		Name:       "Test Series",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSeasonsAndEpisodes(ctx, 100, []store.SeasonRow{{
		SeriesID:     100,
		SeasonNumber: 1,
		Name:         "Season 1",
	}}, []store.EpisodeRow{
		{EpisodeID: 1001, SeriesID: 100, SeasonNumber: 1, EpisodeNum: 1, Title: "Episode 1"},
		{EpisodeID: 1002, SeriesID: 100, SeasonNumber: 1, EpisodeNum: 2, Title: "Episode 2"},
	}); err != nil {
		t.Fatal(err)
	}

	server := New(st, nil, catalog.EnqueueConfig{
		MoviesDir: t.TempDir(),
		SeriesDir: t.TempDir(),
	})
	handler, err := server.Handler(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("test")},
	})
	if err != nil {
		t.Fatal(err)
	}

	episodeResponse := httptest.NewRecorder()
	episodeRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/queue/episode",
		strings.NewReader(`{"series_id":100,"episode_id":1001}`),
	)
	episodeRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(episodeResponse, episodeRequest)
	if episodeResponse.Code != http.StatusAccepted {
		t.Fatalf("episode queue status = %d: %s", episodeResponse.Code, episodeResponse.Body.String())
	}

	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(
		statusResponse,
		httptest.NewRequest(http.MethodGet, "/api/queue/episode?id=1001", nil),
	)
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"queued":true`) {
		t.Fatalf("episode status = %d: %s", statusResponse.Code, statusResponse.Body.String())
	}

	seasonResponse := httptest.NewRecorder()
	seasonRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/queue/season",
		strings.NewReader(`{"series_id":100,"season":1}`),
	)
	seasonRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(seasonResponse, seasonRequest)
	if seasonResponse.Code != http.StatusAccepted {
		t.Fatalf("season queue status = %d: %s", seasonResponse.Code, seasonResponse.Body.String())
	}
	if !strings.Contains(seasonResponse.Body.String(), `"queued_count":1`) {
		t.Fatalf("season queue body = %s", seasonResponse.Body.String())
	}

	seriesResponse := httptest.NewRecorder()
	seriesRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/queue/series",
		strings.NewReader(`{"series_id":100}`),
	)
	seriesRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(seriesResponse, seriesRequest)
	if seriesResponse.Code != http.StatusAccepted {
		t.Fatalf("series queue status = %d: %s", seriesResponse.Code, seriesResponse.Body.String())
	}
	if !strings.Contains(seriesResponse.Body.String(), `"queued":true`) {
		t.Fatalf("series queue body = %s", seriesResponse.Body.String())
	}
}
