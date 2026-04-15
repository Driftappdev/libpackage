// Package gopagination provides production-grade pagination utilities:
// cursor-based, offset-based, and keyset pagination with URL helpers,
// metadata, generic slicing, and Link header generation.
package gopagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ---- Page (offset-based) ----------------------------------------------------

// PageParams holds the parsed parameters for offset-based pagination.
type PageParams struct {
	Page    int // 1-based
	PerPage int
	Sort    string
	Order   string // "asc" | "desc"
}

// Offset returns the SQL offset for the current page.
func (p PageParams) Offset() int {
	if p.Page < 1 {
		return 0
	}
	return (p.Page - 1) * p.PerPage
}

// Limit returns the SQL LIMIT value.
func (p PageParams) Limit() int { return p.PerPage }

// PageMeta holds computed pagination metadata for the response envelope.
type PageMeta struct {
	Page       int  `json:"page"`
	PerPage    int  `json:"per_page"`
	Total      int  `json:"total"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

// Links holds hypermedia navigation URLs.
type Links struct {
	Self  string `json:"self,omitempty"`
	First string `json:"first,omitempty"`
	Prev  string `json:"prev,omitempty"`
	Next  string `json:"next,omitempty"`
	Last  string `json:"last,omitempty"`
}

// Config holds global pagination defaults and limits.
type Config struct {
	DefaultPerPage int
	MaxPerPage     int
	DefaultSort    string
	DefaultOrder   string
	PageParam      string
	PerPageParam   string
	SortParam      string
	OrderParam     string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		DefaultPerPage: 20,
		MaxPerPage:     100,
		DefaultSort:    "created_at",
		DefaultOrder:   "desc",
		PageParam:      "page",
		PerPageParam:   "per_page",
		SortParam:      "sort",
		OrderParam:     "order",
	}
}

// ParsePageParams extracts and validates pagination parameters from an HTTP request.
func ParsePageParams(r *http.Request, cfg Config) (PageParams, error) {
	q := r.URL.Query()

	pageStr := q.Get(cfg.PageParam)
	if pageStr == "" {
		pageStr = "1"
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		return PageParams{}, fmt.Errorf("gopagination: %q must be a positive integer", cfg.PageParam)
	}

	perPageStr := q.Get(cfg.PerPageParam)
	if perPageStr == "" {
		perPageStr = strconv.Itoa(cfg.DefaultPerPage)
	}
	perPage, err := strconv.Atoi(perPageStr)
	if err != nil || perPage < 1 {
		return PageParams{}, fmt.Errorf("gopagination: %q must be a positive integer", cfg.PerPageParam)
	}
	if perPage > cfg.MaxPerPage {
		perPage = cfg.MaxPerPage
	}

	sort := q.Get(cfg.SortParam)
	if sort == "" {
		sort = cfg.DefaultSort
	}
	order := strings.ToLower(q.Get(cfg.OrderParam))
	if order != "asc" && order != "desc" {
		order = cfg.DefaultOrder
	}

	return PageParams{Page: page, PerPage: perPage, Sort: sort, Order: order}, nil
}

// NewPageMeta computes PageMeta from params and total count.
func NewPageMeta(p PageParams, total int) PageMeta {
	totalPages := int(math.Ceil(float64(total) / float64(p.PerPage)))
	if totalPages < 1 {
		totalPages = 1
	}
	return PageMeta{
		Page:       p.Page,
		PerPage:    p.PerPage,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    p.Page < totalPages,
		HasPrev:    p.Page > 1,
	}
}

// BuildLinks creates navigation URLs for a paginated resource.
func BuildLinks(r *http.Request, p PageParams, meta PageMeta, cfg Config) Links {
	base := *r.URL
	setPage := func(page int) string {
		q := base.Query()
		q.Set(cfg.PageParam, strconv.Itoa(page))
		q.Set(cfg.PerPageParam, strconv.Itoa(p.PerPage))
		base.RawQuery = q.Encode()
		return base.String()
	}
	links := Links{Self: setPage(p.Page), First: setPage(1), Last: setPage(meta.TotalPages)}
	if meta.HasPrev {
		links.Prev = setPage(p.Page - 1)
	}
	if meta.HasNext {
		links.Next = setPage(p.Page + 1)
	}
	return links
}

// WriteLinkHeader writes a RFC 8288 Link header to the response.
func WriteLinkHeader(w http.ResponseWriter, links Links) {
	var parts []string
	add := func(u, rel string) {
		if u != "" {
			parts = append(parts, fmt.Sprintf(`<%s>; rel="%s"`, u, rel))
		}
	}
	add(links.First, "first")
	add(links.Prev, "prev")
	add(links.Next, "next")
	add(links.Last, "last")
	if len(parts) > 0 {
		w.Header().Set("Link", strings.Join(parts, ", "))
	}
}

// ---- Envelope ---------------------------------------------------------------

// Page is a generic paginated response envelope.
type Page[T any] struct {
	Data  []T      `json:"data"`
	Meta  PageMeta `json:"meta"`
	Links Links    `json:"links,omitempty"`
}

// NewPage constructs a Page from a slice, total count, params, and request.
func NewPage[T any](data []T, total int, params PageParams, r *http.Request, cfg Config) Page[T] {
	meta := NewPageMeta(params, total)
	links := BuildLinks(r, params, meta, cfg)
	return Page[T]{Data: data, Meta: meta, Links: links}
}

// ---- Slice helpers ----------------------------------------------------------

// Paginate returns a sub-slice representing the requested page.
// The returned bool indicates whether there are more pages.
func Paginate[T any](items []T, params PageParams) ([]T, bool) {
	total := len(items)
	offset := params.Offset()
	if offset >= total {
		return nil, false
	}
	end := offset + params.PerPage
	if end > total {
		end = total
	}
	return items[offset:end], end < total
}

// ---- Cursor (keyset) pagination ----------------------------------------------

// Cursor is an opaque, base64-encoded pagination cursor.
type Cursor struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	Extra     string    `json:"extra,omitempty"`
}

// Encode serialises the cursor to a URL-safe base64 string.
func (c Cursor) Encode() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// MustEncode is like Encode but panics on error.
func (c Cursor) MustEncode() string {
	s, err := c.Encode()
	if err != nil {
		panic(err)
	}
	return s
}

// DecodeCursor parses a cursor string.
func DecodeCursor(s string) (Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("gopagination: invalid cursor: %w", err)
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return Cursor{}, fmt.Errorf("gopagination: malformed cursor: %w", err)
	}
	return c, nil
}

// CursorParams holds parsed cursor-based pagination parameters.
type CursorParams struct {
	After  *Cursor
	Before *Cursor
	Limit  int
	Sort   string
	Order  string
}

// ParseCursorParams extracts cursor pagination params from an HTTP request.
// Query parameters: after=<cursor>, before=<cursor>, limit=<n>, sort=<field>, order=asc|desc
func ParseCursorParams(r *http.Request, cfg Config) (CursorParams, error) {
	q := r.URL.Query()
	params := CursorParams{Sort: cfg.DefaultSort, Order: cfg.DefaultOrder}

	limitStr := q.Get("limit")
	if limitStr == "" {
		params.Limit = cfg.DefaultPerPage
	} else {
		n, err := strconv.Atoi(limitStr)
		if err != nil || n < 1 {
			return params, fmt.Errorf("gopagination: limit must be a positive integer")
		}
		if n > cfg.MaxPerPage {
			n = cfg.MaxPerPage
		}
		params.Limit = n
	}

	if s := q.Get(cfg.SortParam); s != "" {
		params.Sort = s
	}
	if o := strings.ToLower(q.Get(cfg.OrderParam)); o == "asc" || o == "desc" {
		params.Order = o
	}

	if afterStr := q.Get("after"); afterStr != "" {
		c, err := DecodeCursor(afterStr)
		if err != nil {
			return params, err
		}
		params.After = &c
	}
	if beforeStr := q.Get("before"); beforeStr != "" {
		c, err := DecodeCursor(beforeStr)
		if err != nil {
			return params, err
		}
		params.Before = &c
	}

	return params, nil
}

// CursorPage is the response envelope for cursor-based pagination.
type CursorPage[T any] struct {
	Data       []T    `json:"data"`
	HasNext    bool   `json:"has_next"`
	HasPrev    bool   `json:"has_prev"`
	NextCursor string `json:"next_cursor,omitempty"`
	PrevCursor string `json:"prev_cursor,omitempty"`
}

// ---- URL builder helpers ----------------------------------------------------

// AddPaginationQuery adds pagination query parameters to a base URL string.
func AddPaginationQuery(baseURL string, params map[string]string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ---- HTTP helpers -----------------------------------------------------------

// WriteJSON writes a JSON-encoded body with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// RespondPage writes a Page as JSON and sets the Link header.
func RespondPage[T any](w http.ResponseWriter, page Page[T]) error {
	l := page.Links
	var parts []string
	addLink := func(u, rel string) {
		if u != "" {
			parts = append(parts, fmt.Sprintf(`<%s>; rel="%s"`, u, rel))
		}
	}
	addLink(l.First, "first")
	addLink(l.Prev, "prev")
	addLink(l.Next, "next")
	addLink(l.Last, "last")
	if len(parts) > 0 {
		w.Header().Set("Link", strings.Join(parts, ", "))
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(page.Meta.Total))
	w.Header().Set("X-Total-Pages", strconv.Itoa(page.Meta.TotalPages))
	return WriteJSON(w, http.StatusOK, page)
}
