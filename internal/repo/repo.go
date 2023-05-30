package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/jmoiron/sqlx"

	"media-dl-tg/internal/types"
)

const (
	driver = "sqlite3"

	ModeMemory = "memory"
	ModeRWC    = "rwc"
)

// todo: add contexts to functions
type UsersRepository interface {
	Create(tgUserID int64) (*types.User, error)
	Get(tgUserID int64) (*types.User, error)
}

// todo: add contexts to functions
type MediaRepository interface {
	Create(userID int64, tgMessageID int, uri string, typ types.MediaType) (*types.Media, error)
	GetInProgress(userID int64) ([]*types.Media, error)
	UpdateTitle(id int64, title string) error
	UpdateState(id int64, state types.MediaState) error
	DeleteInProgress() error
}

func OpenDBAndMigrate(filePath, mode string) (*sqlx.DB, error) {
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
	m, err := migrate.NewWithDatabaseInstance("file://migrations", driver, drv)
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

//nolint:govet // for better reading and keep as it in .sql files
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

func (u user) ToTypes() types.User { return types.User{} }

type usersRepository struct {
	db *sqlx.DB
}

func (r *usersRepository) Create(tgUserID int64) (*types.User, error) {
	user := &types.User{}
	if err := r.db.Get(user, "insert into users (tg_user_id) VALUES ($1) returning *", tgUserID); err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}
	return user, nil
}

func (r *usersRepository) Get(tgUserID int64) (*types.User, error) {
	user := &types.User{}
	if err := r.db.Get(user, "select * from users where tg_user_id = $1", tgUserID); err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}
	return user, nil
}

//nolint:govet // for better reading and keep as it in .sql files
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

func (m media) ToTypes() types.Media { return types.Media{} }

type mediaRepository struct {
	db *sqlx.DB
}

func (r *mediaRepository) Create(
	userID int64, tgMessageID int, uri string, typ types.MediaType,
) (*types.Media, error) {

	media := &types.Media{}
	if err := r.db.Get(media, `
		insert into media (user_id, tg_message_id, uri, type) VALUES ($1, $2, $3, $4) returning *
	`,
		userID, tgMessageID, uri, typ,
	); err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}
	return media, nil
}

func (r *mediaRepository) GetInProgress(userID int64) ([]*types.Media, error) {
	multimedia := make([]*types.Media, 0)
	if err := r.db.Select(
		&multimedia, "select * from media where user_id = $1 and state = 'pending'", userID,
	); err != nil {
		return nil, fmt.Errorf("failed to select: %w", err)
	}
	return multimedia, nil
}

func (r *mediaRepository) UpdateTitle(id int64, title string) error {
	if _, err := r.db.Exec("update media set title = $1 where id = $2", title, id); err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}
	return nil
}

func (r *mediaRepository) UpdateState(id int64, state types.MediaState) error {
	doneAt := sql.NullTime{}
	if state == types.DoneMediaState {
		doneAt = sql.NullTime{
			Time:  time.Now().UTC(),
			Valid: true,
		}
	}
	if _, err := r.db.Exec(
		"update media set state = $1, updated_at = current_timestamp, done_at = $2 where id = $3",
		state, doneAt, id,
	); err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}
	return nil
}

func (r *mediaRepository) DeleteInProgress() error {
	_, err := r.db.Exec("delete from media where state = 'pending'")
	if err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}
	return nil
}
