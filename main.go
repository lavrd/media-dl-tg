package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jmoiron/sqlx"
)

const (
	dbDriver     = "sqlite3"
	dbModeMemory = "memory"
	dbModeRWC    = "rwc"
)

func openDBAndMigrate(filePath, mode string) (*sqlx.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?cache=shared&mode=%s&_foreign_keys=1",
		filePath, mode,
	)
	db, err := sql.Open(dbDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	/*
		Do database structure migration.
	*/
	driver, err := sqlite3.WithInstance(db, &sqlite3.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to create new driver: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://migrations", dbDriver, driver)
	if err != nil {
		return nil, fmt.Errorf("failed to create new migration manager: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return nil, fmt.Errorf("failed to do database structure migration: %w", err)
	}
	return sqlx.NewDb(db, dbDriver), nil
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

type userRepository struct {
	db *sqlx.DB
}

func (r *userRepository) create(tgUserID int64) (*user, error) {
	user := &user{}
	if err := r.db.Get(user, "insert into users (tg_user_id) VALUES ($1) returning *", tgUserID); err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}
	return user, nil
}

func (r *userRepository) get(tgUserID int64) (*user, error) {
	user := &user{}
	if err := r.db.Get(user, "select * from users where tg_user_id = $1", tgUserID); err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}
	return user, nil
}

type mediaState string

const (
	pendingMediaState mediaState = "pending"
	errorMediaState   mediaState = "error"
	doneMediaState    mediaState = "done"
)

type mediaType string

const (
	audioMediaType mediaType = "audio"
	videoMediaType mediaType = "video"
)

//nolint:govet // for better reading and keep as it in .sql files
type media struct {
	ID     int64 `db:"id"`
	UserID int64 `db:"user_id"`
	// Telegram message id in telegram to understand to what message we need to reply.
	TgMessageID int `db:"tg_message_id"`
	// URI to download video.
	URI       string     `db:"uri"`
	Title     string     `db:"title"`
	State     mediaState `db:"state"`
	MediaType mediaType  `db:"media_type"`
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt time.Time  `db:"updated_at"`
	// When media was successfully downloaded.
	DoneAt *time.Time `db:"done_at"`
}

type mediaRepository struct {
	db *sqlx.DB
}

func (r *mediaRepository) create(userID int64, tgMessageID int, uri string, mediaType mediaType) (*media, error) {
	media := &media{}
	if err := r.db.Get(media, `
		insert into media (user_id, tg_message_id, uri, media_type) VALUES ($1, $2, $3, $4) returning *
	`,
		userID, tgMessageID, uri, mediaType,
	); err != nil {
		return nil, fmt.Errorf("failed to get: %w", err)
	}
	return media, nil
}

func (r *mediaRepository) getInProgress(userID int64) ([]*media, error) {
	multimedia := make([]*media, 0)
	if err := r.db.Select(
		&multimedia, "select * from media where user_id = $1 and state = 'pending'", userID,
	); err != nil {
		return nil, fmt.Errorf("failed to select: %w", err)
	}
	return multimedia, nil
}

func (r *mediaRepository) updateTitle(id int64, title string) error {
	if _, err := r.db.Exec("update media set title = $1 where id = $2", title, id); err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}
	return nil
}

func (r *mediaRepository) updateState(id int64, state mediaState) error {
	doneAt := sql.NullTime{}
	if state == doneMediaState {
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

func (r *mediaRepository) deleteInProgress() error {
	_, err := r.db.Exec("delete from media where state = 'pending'")
	if err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}
	return nil
}
