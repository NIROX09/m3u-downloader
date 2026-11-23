package store

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertCategories(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cats := []CategoryRow{
		{ID: 1, Type: "vod", Name: "Action"},
		{ID: 2, Type: "vod", Name: "Drama"},
	}
	if err := s.UpsertCategories(ctx, cats); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListCategories(ctx, "vod")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d categories, want 2", len(got))
	}
	cats[0].Name = "Action!"
	if err := s.UpsertCategories(ctx, cats); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListCategories(ctx, "vod")
	if len(got) != 2 || got[0].Name != "Action!" {
		t.Errorf("upsert did not update: %+v", got)
	}
}

func TestUpsertVODs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertCategories(ctx, []CategoryRow{{ID: 1, Type: "vod", Name: "A"}}); err != nil {
		t.Fatal(err)
	}
	vods := []VODRow{
		{StreamID: 100, CategoryID: 1, Name: "M1", Year: 2020, ContainerExt: "mkv"},
		{StreamID: 101, CategoryID: 1, Name: "M2", Year: 2021, ContainerExt: "ts"},
	}
	if err := s.UpsertVODs(ctx, vods); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListVODs(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d vods, want 2", len(got))
	}
}

func TestAdultFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	categories := []CategoryRow{
		{ID: 1, Type: "vod", Name: "Action"},
		{ID: 2, Type: "vod", Name: "FOR ADULTS ➾ ANAL"},
	}
	if err := s.UpsertCategories(ctx, categories); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertVODs(ctx, []VODRow{
		{StreamID: 100, CategoryID: 1, Name: "Safe Movie", ContainerExt: "mkv"},
		{StreamID: 101, CategoryID: 2, Name: "Adult Movie", ContainerExt: "mkv"},
	}); err != nil {
		t.Fatal(err)
	}

	filteredCategories, err := s.ListCategoriesFiltered(ctx, "vod", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredCategories) != 1 || filteredCategories[0].Name != "Action" {
		t.Fatalf("filtered categories = %+v", filteredCategories)
	}
	unfilteredCategories, err := s.ListCategoriesFiltered(ctx, "vod", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(unfilteredCategories) != 2 {
		t.Fatalf("unfiltered categories = %d, want 2", len(unfilteredCategories))
	}

	filteredMovies, total, err := s.ListVODPageFiltered(ctx, 0, "", 50, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(filteredMovies) != 1 || filteredMovies[0].Name != "Safe Movie" || total != 1 {
		t.Fatalf("filtered movies = %+v, total = %d", filteredMovies, total)
	}

	unfilteredMovies, total, err := s.ListVODPageFiltered(ctx, 0, "", 50, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(unfilteredMovies) != 2 || total != 2 {
		t.Fatalf("unfiltered movies = %+v, total = %d", unfilteredMovies, total)
	}
}

func TestVODSorting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertVODs(ctx, []VODRow{
		{StreamID: 1, CategoryID: 1, Name: "Zulu", Year: 2010, Rating: 5},
		{StreamID: 2, CategoryID: 1, Name: "Alpha", Year: 2024, Rating: 9},
		{StreamID: 3, CategoryID: 1, Name: "Beta", Year: 2020, Rating: 7},
	}); err != nil {
		t.Fatal(err)
	}

	byName, _, err := s.ListVODPageSorted(ctx, 0, "", 50, 0, false, "name")
	if err != nil {
		t.Fatal(err)
	}
	if byName[0].Name != "Alpha" {
		t.Fatalf("name sort first = %q", byName[0].Name)
	}

	byYear, _, err := s.ListVODPageSorted(ctx, 0, "", 50, 0, false, "year_desc")
	if err != nil {
		t.Fatal(err)
	}
	if byYear[0].Name != "Alpha" {
		t.Fatalf("year sort first = %q", byYear[0].Name)
	}

	byRating, _, err := s.ListVODPageSorted(ctx, 0, "", 50, 0, false, "rating_desc")
	if err != nil {
		t.Fatal(err)
	}
	if byRating[0].Name != "Alpha" {
		t.Fatalf("rating sort first = %q", byRating[0].Name)
	}
}
