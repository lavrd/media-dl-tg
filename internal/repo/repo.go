package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file" // we need this import to use file driver for migration tool
	"github.com/jmoiron/sqlx"

	"media-dl-tg/internal/types"
)

const driver = "sqlite3"

type Mode string

const (
	ModeMemory Mode = "memory"
	ModeRWC    Mode = "rwc"
)

type UsersRepository interface {
	Create(ctx context.Context, tgUserID int64) (*types.User, error)
	Get(ctx context.Context, tgUserID int64) (*types.User, error)
}

type MediaRepository interface {
	Create(ctx context.Context, userID int64, tgMessageID int, uri string, typ types.MediaType) (*types.Media, error)
	GetInProgress(ctx context.Context, userID int64) ([]*types.Media, error)
	UpdateTitle(ctx context.Context, id int64, title string) error
	UpdateState(ctx context.Context, id int64, state types.MediaState) error
	DeleteInProgress(ctx context.Context) error
}

func OpenDBAndMigrate(filePath string, mode Mode) (*sqlx.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?cache=shared&mode=%s&_foreign_keys=1",
		filePath, mode,
	)
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	// Do database structure migration.
	drv, err := sqlite3.WithInstance(db, &sqlite3.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to create new driver: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://../../migrations", driver, drv)
	if err != nil {
		return nil, fmt.Errorf("failed to create new migration manager: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return nil, fmt.Errorf("failed to do database structure migration: %w", err)
	}
	return sqlx.NewDb(db, driver), nil
}

func New(db *sqlx.DB) (UsersRepository, MediaRepository) {
	usersRepo := &usersRepository{db}
	mediaReo := &mediaRepository{db}
	return usersRepo, mediaReo
}

//nolint:govet // disable field aligment for better reading and keep as it in .sql files
type user struct {
	ID       int64 `db:"id"`
	TgUserID int64 `db:"tg_user_id"`
	// Maximum allowable size for downloading audio for a user.
	AudioMaxSize int64 `db:"audio_max_size"`
	// Maximum allowable size for downloading video for a user.
	VideoMaxSize int64 `db:"video_max_size"`
	// Maximum allowable videos length to download from playlist.
	PlaylistMaxSize int64     `db:"playlist_max_size"`
	CreatedAt       time.Time `db:"created_at"`
	// Last message from the user in the bot.
	LastEventAt time.Time `db:"last_event_at"`
}

func (u *user) ToTypes() *types.User {
	return &types.User{
		ID:              u.ID,
		TgUserID:        u.TgUserID,
		AudioMaxSize:    u.AudioMaxSize,
		VideoMaxSize:    u.VideoMaxSize,
		PlaylistMaxSize: u.PlaylistMaxSize,
		CreatedAt:       u.CreatedAt,
		LastEventAt:     u.LastEventAt,
	}
}

type usersRepository struct {
	db *sqlx.DB
}

func (r *usersRepository) Create(ctx context.Context, tgUserID int64) (*types.User, error) {
	user := &user{}
	if err := r.db.GetContext(ctx, user, "insert into users (tg_user_id) VALUES ($1) returning *", tgUserID); err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}
	return user.ToTypes(), nil
}

func (r *usersRepository) Get(ctx context.Context, tgUserID int64) (*types.User, error) {
	user := &user{}
	if err := r.db.GetContext(ctx, user, "select * from users where tg_user_id = $1", tgUserID); err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}
	return user.ToTypes(), nil
}

//nolint:govet // disable field aligment for better reading and keep as it in .sql files
type media struct {
	ID     int64 `db:"id"`
	UserID int64 `db:"user_id"`
	// Telegram message id in telegram to understand to what message we need to reply.
	TgMessageID int `db:"tg_message_id"`
	// URI to download video.
	URI       string           `db:"uri"`
	Title     string           `db:"title"`
	State     types.MediaState `db:"state"`
	Type      types.MediaType  `db:"type"`
	CreatedAt time.Time        `db:"created_at"`
	UpdatedAt time.Time        `db:"updated_at"`
	// When media was successfully downloaded.
	DoneAt *time.Time `db:"done_at"`
}

func (m *media) ToTypes() *types.Media {
	return &types.Media{
		ID:          m.ID,
		UserID:      m.UserID,
		TgMessageID: m.TgMessageID,
		URI:         m.URI,
		Title:       m.Title,
		State:       m.State,
		Type:        m.Type,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		DoneAt:      m.DoneAt,
	}
}

type mediaRepository struct {
	db *sqlx.DB
}

func (r *mediaRepository) Create(
	ctx context.Context,
	userID int64, tgMessageID int, uri string, typ types.MediaType,
) (*types.Media, error) {

	media := &media{}
	if err := r.db.GetContext(ctx, media, `
		insert into media (user_id, tg_message_id, uri, type) VALUES ($1, $2, $3, $4) returning *
	`,
		userID, tgMessageID, uri, typ,
	); err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}
	return media.ToTypes(), nil
}

func (r *mediaRepository) GetInProgress(ctx context.Context, userID int64) ([]*types.Media, error) {
	multimedia := make([]*media, 0)
	if err := r.db.SelectContext(ctx, &multimedia,
		"select * from media where user_id = $1 and state = 'pending'", userID,
	); err != nil {
		return nil, fmt.Errorf("failed to select: %w", err)
	}
	out := make([]*types.Media, len(multimedia))
	for i, m := range multimedia {
		out[i] = m.ToTypes()
	}
	return out, nil
}

func (r *mediaRepository) UpdateTitle(ctx context.Context, id int64, title string) error {
	if _, err := r.db.ExecContext(ctx, "update media set title = $1 where id = $2", title, id); err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}
	return nil
}

func (r *mediaRepository) UpdateState(ctx context.Context, id int64, state types.MediaState) error {
	doneAt := sql.NullTime{}
	if state == types.DoneMediaState {
		doneAt = sql.NullTime{
			Time:  time.Now().UTC(),
			Valid: true,
		}
	}
	if _, err := r.db.ExecContext(ctx,
		"update media set state = $1, updated_at = current_timestamp, done_at = $2 where id = $3",
		state, doneAt, id,
	); err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}
	return nil
}

func (r *mediaRepository) DeleteInProgress(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, "delete from media where state = 'pending'")
	if err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}
	return nil
}
