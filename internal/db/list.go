package db

import "fmt"

// ListOptions controls server-side sorting, filtering, and pagination for
// ListContent. JSON tags are camelCase for the Wails binding. The zero value
// (all empty) is valid: it yields the default sort with no filters and no
// pagination cap.
type ListOptions struct {
	// SortBy is a UI sort key, not a column name. Valid values are the keys of
	// sortColumns; anything else falls back to the default ("downloadDate").
	SortBy string `json:"sortBy"`
	// SortDir is "asc" or "desc" (default "desc").
	SortDir string `json:"sortDir"`
	// Limit caps the page size; <= 0 means "no limit" (return all matches).
	Limit int `json:"limit"`
	// Offset is the row offset for paging; ignored when Limit <= 0.
	Offset int `json:"offset"`
	// Source, when non-empty, filters to a single source (extractor key).
	Source string `json:"source"`
	// PlaylistID, when non-zero, filters to one playlist grouping.
	PlaylistID int64 `json:"playlistId"`
}

// ListResult is a page of content plus the total count of rows matching the
// filters (before limit/offset), which drives the frontend pager.
type ListResult struct {
	Items []Content `json:"items"`
	Total int       `json:"total"`
}

// sortColumns whitelists the UI sort keys → SQL columns. SortBy/SortDir are
// NEVER interpolated raw into SQL; only values found here (and the validated
// direction) reach the query, which is the injection guard.
var sortColumns = map[string]string{
	"source":       "c.source",
	"title":        "c.title",
	"channel":      "c.uploader",
	"publishDate":  "c.upload_date",
	"downloadDate": "c.downloaded_at",
}

const defaultSortBy = "downloadDate"

// resolveSort returns the (column, direction) to use, defaulting unknown keys
// to downloadDate and unknown directions to desc. The id tiebreaker keeps
// pagination stable when the sort column has ties.
func resolveSort(sortBy, sortDir string) (col, dir string) {
	col, ok := sortColumns[sortBy]
	if !ok {
		col = sortColumns[defaultSortBy]
	}
	dir = "DESC"
	if sortDir == "asc" {
		dir = "ASC"
	}
	return col, dir
}

// ListContent returns a sorted, filtered, paginated page of content together
// with the total number of rows matching the filters. Sorting is done in SQL
// against a whitelisted column; fuzzy search is deliberately NOT here — it runs
// client-side over the loaded page (no FTS5 in this driver).
func (s *Store) ListContent(opts ListOptions) (ListResult, error) {
	col, dir := resolveSort(opts.SortBy, opts.SortDir)

	// Build the shared WHERE clause + args from the optional filters.
	where := ""
	var args []any
	add := func(clause string, val any) {
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		where += clause
		args = append(args, val)
	}
	if opts.Source != "" {
		add("c.source = ?", opts.Source)
	}
	if opts.PlaylistID != 0 {
		add("c.playlist_id = ?", opts.PlaylistID)
	}

	// Total first (before limit/offset) so the pager is correct.
	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM content c"+where, args...).
		Scan(&total); err != nil {
		return ListResult{}, fmt.Errorf("count content: %w", err)
	}

	// col/dir come from the whitelist + validated direction, so this Sprintf is
	// injection-safe. The id tiebreaker stabilises ties for paging.
	query := "SELECT " + contentColumns + contentFromJoin + where +
		fmt.Sprintf(" ORDER BY %s %s, c.id %s", col, dir, dir)

	queryArgs := args
	if opts.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		queryArgs = append(append([]any{}, args...), opts.Limit, max0(opts.Offset))
	}

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list content: %w", err)
	}
	defer rows.Close()

	items := []Content{}
	for rows.Next() {
		c, err := scanContent(rows)
		if err != nil {
			return ListResult{}, fmt.Errorf("scan content: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("iterate content: %w", err)
	}

	return ListResult{Items: items, Total: total}, nil
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
