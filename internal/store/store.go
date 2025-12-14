package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("not found")
var ErrDuplicate = errors.New("duplicate asset")

const defaultPageSize = 30

var allowedSort = map[string]string{
	"newest":    "created_at DESC",
	"oldest":    "created_at ASC",
	"relevance": "relevance DESC, created_at DESC",
}

type Store struct {
	db *sqlx.DB
}

func New(db *sqlx.DB) *Store {
	return &Store{db: db}
}

func (s *Store) DB() *sqlx.DB {
	return s.db
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) CreateAsset(ctx context.Context, in AssetCreate) (*Asset, error) {
	tags := NormalizeTags(in.Tags)
	tagText := TagText(tags)

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	query := `INSERT INTO asset (title, caption, credit, source, usage_notes, width, height, bytes, mime, original_filename, sha256, tag_text)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := tx.ExecContext(ctx, query,
		in.Title, in.Caption, in.Credit, in.Source, in.UsageNotes,
		in.Width, in.Height, in.Bytes, in.Mime, in.OriginalFilename, in.SHA256, tagText,
	)
	if err != nil {
		// Duplicate hash? return conflict by fetching existing asset.
		if isDuplicate(err) {
			existing, getErr := s.getAssetByHash(ctx, tx, in.SHA256)
			if getErr == nil {
				return existing, ErrDuplicate
			}
			return nil, ErrDuplicate
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	if err := s.replaceTagsTx(ctx, tx, id, tags, tagText); err != nil {
		return nil, err
	}

	asset, err := s.getAssetByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return asset, nil
}

func (s *Store) getAssetByHash(ctx context.Context, tx *sqlx.Tx, sha string) (*Asset, error) {
	return s.fetchAsset(ctx, tx, "sha256 = ?", sha)
}

func (s *Store) getAssetByID(ctx context.Context, tx *sqlx.Tx, id int64) (*Asset, error) {
	return s.fetchAsset(ctx, tx, "id = ?", id)
}

func (s *Store) fetchAsset(ctx context.Context, tx *sqlx.Tx, where string, arg any) (*Asset, error) {
	query := "SELECT id, title, caption, credit, source, usage_notes, width, height, bytes, mime, original_filename, sha256, tag_text, created_at, updated_at, deleted_at FROM asset WHERE " + where
	var a Asset
	var err error
	if tx != nil {
		err = tx.GetContext(ctx, &a, query, arg)
	} else {
		err = s.db.GetContext(ctx, &a, query, arg)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.attachTags(ctx, tx, []*Asset{&a}); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) GetAsset(ctx context.Context, id int64, includeDeleted bool) (*Asset, error) {
	where := "id = ?"
	if !includeDeleted {
		where += " AND deleted_at IS NULL"
	}
	return s.fetchAsset(ctx, nil, where, id)
}

func (s *Store) UpdateAsset(ctx context.Context, id int64, upd AssetUpdate) (*Asset, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	setParts := []string{}
	args := []any{}
	if upd.Title != nil {
		setParts = append(setParts, "title = ?")
		args = append(args, *upd.Title)
	}
	if upd.Caption != nil {
		setParts = append(setParts, "caption = ?")
		args = append(args, *upd.Caption)
	}
	if upd.Credit != nil {
		setParts = append(setParts, "credit = ?")
		args = append(args, *upd.Credit)
	}
	if upd.Source != nil {
		setParts = append(setParts, "source = ?")
		args = append(args, *upd.Source)
	}
	if upd.UsageNotes != nil {
		setParts = append(setParts, "usage_notes = ?")
		args = append(args, *upd.UsageNotes)
	}

	var tags []string
	if upd.Tags != nil {
		tags = NormalizeTags(*upd.Tags)
		setParts = append(setParts, "tag_text = ?")
		args = append(args, TagText(tags))
	}

	if len(setParts) > 0 {
		setParts = append(setParts, "updated_at = NOW()")
		query := "UPDATE asset SET " + strings.Join(setParts, ", ") + " WHERE id = ? AND deleted_at IS NULL"
		args = append(args, id)
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return nil, ErrNotFound
		}
	}

	if upd.Tags != nil {
		if err := s.replaceTagsTx(ctx, tx, id, tags, TagText(tags)); err != nil {
			return nil, err
		}
	}

	asset, err := s.getAssetByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return asset, nil
}

func (s *Store) DeleteAsset(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "UPDATE asset SET deleted_at = NOW(), updated_at = NOW() WHERE id = ? AND deleted_at IS NULL", id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) replaceTagsTx(ctx context.Context, tx *sqlx.Tx, assetID int64, tags []string, tagText string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM asset_tag WHERE asset_id = ?", assetID); err != nil {
		return err
	}

	if len(tags) == 0 {
		_, err := tx.ExecContext(ctx, "UPDATE asset SET tag_text = ?, updated_at = NOW() WHERE id = ?", tagText, assetID)
		return err
	}

	for _, t := range tags {
		var tagID int64
		res, err := tx.ExecContext(ctx, "INSERT INTO tag (name) VALUES (?) ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)", t)
		if err != nil {
			return err
		}
		tagID, err = res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT IGNORE INTO asset_tag (asset_id, tag_id) VALUES (?, ?)", assetID, tagID); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE asset SET tag_text = ?, updated_at = NOW() WHERE id = ?", tagText, assetID)
	return err
}

func (s *Store) SearchAssets(ctx context.Context, params SearchParams) ([]Asset, int, error) {
	page := params.Page
	if page <= 0 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	offset := (page - 1) * pageSize

	where := []string{"1=1"}
	args := []any{}
	if !params.IncludeDeleted {
		where = append(where, "a.deleted_at IS NULL")
	}

	relevanceSelect := ""
	if params.Query != "" {
		where = append(where, "MATCH(a.title, a.caption, a.tag_text) AGAINST (? IN NATURAL LANGUAGE MODE)")
		args = append(args, params.Query)
		relevanceSelect = ", MATCH(a.title, a.caption, a.tag_text) AGAINST (? IN NATURAL LANGUAGE MODE) AS relevance"
	}

	join := ""
	having := ""
	if len(params.Tags) > 0 {
		tags := NormalizeTags(params.Tags)
		if len(tags) > 0 {
			placeholders := strings.Repeat("?,", len(tags))
			placeholders = strings.TrimSuffix(placeholders, ",")
			join = "JOIN asset_tag at ON at.asset_id = a.id JOIN tag t ON t.id = at.tag_id"
			where = append(where, "t.name IN ("+placeholders+")")
			for _, t := range tags {
				args = append(args, t)
			}
			having = "HAVING COUNT(DISTINCT t.name) = ?"
			args = append(args, len(tags))
		}
	}

	orderClause := allowedSort[params.Sort]
	if orderClause == "" {
		orderClause = allowedSort["newest"]
	}
	if params.Sort == "relevance" && params.Query == "" {
		orderClause = allowedSort["newest"]
	}

	whereSQL := strings.Join(where, " AND ")
	base := "FROM asset a " + join + " WHERE " + whereSQL

	var total int
	if having != "" {
		countQuery := "SELECT COUNT(*) FROM (SELECT a.id " + base + " GROUP BY a.id " + having + ") sub"
		if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
			return nil, 0, err
		}
	} else {
		countQuery := "SELECT COUNT(DISTINCT a.id) " + base
		if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
			return nil, 0, err
		}
	}

	selectQuery := "SELECT a.id, a.title, a.caption, a.credit, a.source, a.usage_notes, a.width, a.height, a.bytes, a.mime, a.original_filename, a.sha256, a.tag_text, a.created_at, a.updated_at, a.deleted_at" + relevanceSelect + " " + base + " GROUP BY a.id " + having + " ORDER BY " + orderClause + " LIMIT ? OFFSET ?"
	listArgs := []any{}
	if relevanceSelect != "" {
		listArgs = append(listArgs, params.Query)
	}
	listArgs = append(listArgs, args...)
	listArgs = append(listArgs, pageSize, offset)

	var rows []Asset
	if err := s.db.SelectContext(ctx, &rows, selectQuery, listArgs...); err != nil {
		return nil, 0, err
	}

	assets := make([]*Asset, len(rows))
	for i := range rows {
		assets[i] = &rows[i]
	}
	if err := s.attachTags(ctx, nil, assets); err != nil {
		return nil, 0, err
	}

	return rows, total, nil
}

func (s *Store) attachTags(ctx context.Context, tx *sqlx.Tx, assets []*Asset) error {
	if len(assets) == 0 {
		return nil
	}
	ids := make([]int64, len(assets))
	index := make(map[int64]*Asset)
	for i, a := range assets {
		ids[i] = a.ID
		index[a.ID] = a
	}

	placeholders := strings.Repeat("?,", len(ids))
	placeholders = strings.TrimSuffix(placeholders, ",")
	query := "SELECT at.asset_id, t.name FROM asset_tag at JOIN tag t ON t.id = at.tag_id WHERE at.asset_id IN (" + placeholders + ") ORDER BY t.name"
	rows, err := (func() (*sqlx.Rows, error) {
		if tx != nil {
			return tx.QueryxContext(ctx, query, toAny(ids)...)
		}
		return s.db.QueryxContext(ctx, query, toAny(ids)...)
	})()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID int64
		var name string
		if err := rows.Scan(&assetID, &name); err != nil {
			return err
		}
		index[assetID].Tags = append(index[assetID].Tags, name)
	}
	return rows.Err()
}

func toAny[T comparable](vals []T) []any {
	res := make([]any, len(vals))
	for i, v := range vals {
		res[i] = v
	}
	return res
}

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique")
}

func (s *Store) ListTags(ctx context.Context, prefix string, page, pageSize int) ([]string, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	where := ""
	args := []any{}
	if prefix != "" {
		where = "WHERE name LIKE ?"
		args = append(args, prefix+"%")
	}

	countQuery := "SELECT COUNT(*) FROM tag " + where
	var total int
	if err := s.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, 0, err
	}

	query := "SELECT name FROM tag " + where + " ORDER BY name LIMIT ? OFFSET ?"
	argsWithPaging := append(append([]any{}, args...), pageSize, offset)
	var tags []string
	if err := s.db.SelectContext(ctx, &tags, query, argsWithPaging...); err != nil {
		return nil, 0, err
	}
	return tags, total, nil
}
