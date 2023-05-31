package repo_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"media-dl-tg/internal/repo"
	"media-dl-tg/internal/types"
)

const (
	tgUserID    int64 = 249191443
	tgMessageID int   = 554555
	uri               = "https://example.com/media/213"
)

func TestUserRepository(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	db, err := repo.OpenDBAndMigrate("", repo.ModeMemory)
	r.NoError(err)
	defer func() {
		if err = db.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close database")
		}
	}()
	usersRepo, _ := repo.New(db)

	user, err := usersRepo.Create(ctx, tgUserID)
	r.NoError(err)
	r.Equal(int64(1), user.ID)
	r.Equal(tgUserID, user.TgUserID)
	r.Equal(int64(0), user.AudioMaxSize)
	r.Equal(int64(0), user.VideoMaxSize)
	r.Equal(int64(0), user.PlaylistMaxSize)
	r.False(user.CreatedAt.IsZero())
	r.False(user.LastEventAt.IsZero())

	// Check 409.
	_, err = usersRepo.Create(ctx, tgUserID)
	r.Error(err)
	r.Contains(err.Error(), "UNIQUE")

	user, err = usersRepo.Get(ctx, tgUserID)
	r.NoError(err)
	r.Equal(int64(1), user.ID)
	r.Equal(tgUserID, user.TgUserID)
	r.Equal(int64(0), user.AudioMaxSize)
	r.Equal(int64(0), user.VideoMaxSize)
	r.Equal(int64(0), user.PlaylistMaxSize)
	r.False(user.CreatedAt.IsZero())
	r.False(user.LastEventAt.IsZero())

	// Manually delete user and check 404.
	_, err = db.Exec("delete from users where tg_user_id = $1", tgUserID)
	r.NoError(err)
	_, err = usersRepo.Get(ctx, tgUserID)
	r.Error(err)
	r.True(errors.Is(err, sql.ErrNoRows))
}

func TestMediaRepository(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	db, err := repo.OpenDBAndMigrate("", repo.ModeMemory)
	r.NoError(err)
	defer func() {
		if err = db.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close database")
		}
	}()
	usersRepo, mediaRepo := repo.New(db)

	user, err := usersRepo.Create(ctx, tgUserID)
	r.NoError(err)

	t.Run("test that there are no multimedia at start", func(t *testing.T) {
		multimedia := getMultimedia(r, db, user.ID)
		r.NoError(err)
		r.NotNil(multimedia)
		r.Empty(multimedia)
	})

	var mediaID int64

	t.Run("test multimedia creation", func(t *testing.T) {
		var media *types.Media
		media, err = mediaRepo.Create(ctx, user.ID, tgMessageID, uri, types.AudioMediaType)
		r.NoError(err)
		r.Equal(int64(1), media.ID)
		r.NotEmpty(media.TgMessageID)
		r.NotEmpty(media.URI)
		r.Equal(user.ID, media.UserID)
		r.Equal(types.PendingMediaState, media.State)
		r.Equal(types.AudioMediaType, media.Type)
		r.False(media.CreatedAt.IsZero())
		r.False(media.UpdatedAt.IsZero())
		r.Nil(media.DoneAt)

		mediaID = media.ID
	})

	t.Run("test getting multimedia", func(t *testing.T) {
		internalMedia := getMedia(r, db, mediaID, user.ID)
		r.Equal(int64(1), internalMedia.ID)
		r.Equal(user.ID, internalMedia.UserID)
		r.NotEmpty(internalMedia.TgMessageID)
		r.NotEmpty(internalMedia.URI)
		r.Equal(types.PendingMediaState, internalMedia.State)
		r.Equal(types.AudioMediaType, internalMedia.Type)
		r.False(internalMedia.CreatedAt.IsZero())
		r.False(internalMedia.UpdatedAt.IsZero())
		r.Nil(internalMedia.DoneAt)
	})

	t.Run("test multimedia states", func(t *testing.T) {
		err = mediaRepo.UpdateState(ctx, mediaID, types.ErrorMediaState)
		r.NoError(err)
		internalMedia := getMedia(r, db, mediaID, user.ID)
		r.Equal(types.ErrorMediaState, internalMedia.State)

		err = mediaRepo.UpdateState(ctx, mediaID, types.DoneMediaState)
		r.NoError(err)
		internalMedia = getMedia(r, db, mediaID, user.ID)
		r.Equal(types.DoneMediaState, internalMedia.State)

		err = mediaRepo.UpdateState(ctx, mediaID, "asd")
		r.Error(err)
		r.Contains(err.Error(), "CHECK constraint failed")
	})

	t.Run("test that after creating new multimedia length will be increased", func(t *testing.T) {
		_, err = mediaRepo.Create(ctx, user.ID, tgMessageID, uri, types.VideoMediaType)
		r.NoError(err)

		multimedia := getMultimedia(r, db, user.ID)
		r.NoError(err)
		r.NotNil(multimedia)
		r.Len(multimedia, 2)
	})

	t.Run("test title updating", func(t *testing.T) {
		title := "hello neighbor"
		err = mediaRepo.UpdateTitle(ctx, mediaID, title)
		r.NoError(err)
		internalMedia := getMedia(r, db, mediaID, user.ID)
		r.Equal(title, internalMedia.Title)
	})

	t.Run("test getting pending multimedia", func(t *testing.T) {
		// Check get pending multimedia.
		var multimedia []*types.Media
		multimedia, err = mediaRepo.GetInProgress(ctx, user.ID)
		r.NoError(err)
		r.NotNil(multimedia)
		//nolint:godox // will resolve it later
		// todo: when error state will be like "pending" check that length is 2
		r.Len(multimedia, 1)
		// r.Equal(types.PendingMediaState, multimedia[0].State)
		//nolint:godox // will resolve it later
		// todo: when error state will be like "pending" check that
		// r.Equal(errorMediaState, multimedia[1].State)
	})

	t.Run("test that doneAt is not zero when media is done", func(t *testing.T) {
		_, err = db.Exec("update media set done_at = current_timestamp where id = $1", mediaID)
		r.NoError(err)
		internalMedia := getMedia(r, db, mediaID, user.ID)
		r.NotNil(internalMedia.DoneAt)
		r.False(internalMedia.DoneAt.IsZero())
	})

	t.Run("test creation with incorrect media type", func(t *testing.T) {
		var media *types.Media
		media, err = mediaRepo.Create(ctx, user.ID, tgMessageID, uri, "asd")
		r.Error(err)
		r.Contains(err.Error(), "CHECK constraint failed")
		r.Nil(media)
	})

	t.Run("test unknown user id", func(t *testing.T) {
		var media *types.Media
		media, err = mediaRepo.Create(ctx, 123, tgMessageID, uri, types.AudioMediaType)
		r.Error(err)
		r.Contains(err.Error(), "FOREIGN KEY constraint failed")
		r.Nil(media)
	})

	t.Run("test delete media in progress", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			_, err = mediaRepo.Create(ctx, user.ID, tgMessageID, uri, types.AudioMediaType)
			r.NoError(err)
		}
		var multimedia []*types.Media
		multimedia, err = mediaRepo.GetInProgress(ctx, user.ID)
		r.NoError(err)
		r.Len(multimedia, 4)

		err = mediaRepo.DeleteInProgress(ctx)
		r.NoError(err)
		multimedia, err = mediaRepo.GetInProgress(ctx, user.ID)
		r.NoError(err)
		r.Len(multimedia, 0)
	})

	t.Run("test delete cascade", func(t *testing.T) {
		multimedia := getMultimedia(r, db, user.ID)
		r.NoError(err)
		r.Len(multimedia, 1)
		_, err = db.Exec("delete from users where id = $1", user.ID)
		r.NoError(err)
		multimedia = getMultimedia(r, db, user.ID)
		r.NoError(err)
		r.NotNil(multimedia)
		r.Len(multimedia, 0)
	})
}

func getMedia(r *require.Assertions, db *sqlx.DB, id, userID int64) *media {
	multimedia := getMultimedia(r, db, userID)
	r.NotEmpty(multimedia)
	for _, media := range multimedia {
		if media.ID == id {
			return media
		}
	}
	r.Fail("media is not found")
	return nil
}

func getMultimedia(r *require.Assertions, db *sqlx.DB, userID int64) []*media {
	multimedia := make([]*media, 0)
	err := db.Select(&multimedia, "select * from media where user_id = $1", userID)
	r.NoError(err)
	r.NotNil(multimedia)
	return multimedia
}

// media is a copy of the same structure from repo.go.
//
//nolint:govet // for better reading and keep as it in .sql files
type media struct {
	ID          int64            `db:"id"`
	UserID      int64            `db:"user_id"`
	TgMessageID int              `db:"tg_message_id"`
	URI         string           `db:"uri"`
	Title       string           `db:"title"`
	State       types.MediaState `db:"state"`
	Type        types.MediaType  `db:"type"`
	CreatedAt   time.Time        `db:"created_at"`
	UpdatedAt   time.Time        `db:"updated_at"`
	DoneAt      *time.Time       `db:"done_at"`
}
