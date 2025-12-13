package store

import "time"

type Asset struct {
	ID               int64      `db:"id"`
	Title            string     `db:"title"`
	Caption          string     `db:"caption"`
	Credit           string     `db:"credit"`
	Source           string     `db:"source"`
	UsageNotes       string     `db:"usage_notes"`
	Width            int        `db:"width"`
	Height           int        `db:"height"`
	Bytes            int64      `db:"bytes"`
	Mime             string     `db:"mime"`
	OriginalFilename string     `db:"original_filename"`
	SHA256           string     `db:"sha256"`
	TagText          string     `db:"tag_text"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
	DeletedAt        *time.Time `db:"deleted_at"`
	Relevance        *float64   `db:"relevance"`
	Tags             []string   `db:"-"`
}

type AssetCreate struct {
	Title            string
	Caption          string
	Credit           string
	Source           string
	UsageNotes       string
	Tags             []string
	Width            int
	Height           int
	Bytes            int64
	Mime             string
	OriginalFilename string
	SHA256           string
}

type AssetUpdate struct {
	Title      *string
	Caption    *string
	Credit     *string
	Source     *string
	UsageNotes *string
	Tags       *[]string
}

type SearchParams struct {
	Query          string
	Tags           []string
	Page           int
	PageSize       int
	Sort           string
	IncludeDeleted bool
}
