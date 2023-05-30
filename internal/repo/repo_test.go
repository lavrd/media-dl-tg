package repo_test

import (
	"database/sql"
	"errors"
	"testing"

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

	db, err := repo.OpenDBAndMigrate("", repo.ModeMemory)
	r.NoError(err)
	defer func() {
		if err = db.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close database")
		}
	}()
	usersRepo, _ := repo.New(db)

	user, err := usersRepo.Create(tgUserID)
	r.NoError(err)
	r.Equal(int64(1), user.ID)
	// r.Equal(tgUserID, user.TgUserID)
	r.Equal(int64(0), user.AudioMaxSize)
	r.Equal(int64(0), user.VideoMaxSize)
	r.Equal(int64(0), user.PlaylistMaxSize)
	// r.False(user.CreatedAt.IsZero())
	// r.False(user.LastEventAt.IsZero())

	// Check 409.
	_, err = usersRepo.Create(tgUserID)
	r.Error(err)
	r.Contains(err.Error(), "UNIQUE")

	user, err = usersRepo.Get(tgUserID)
	r.NoError(err)
	r.Equal(int64(1), user.ID)
	// r.Equal(tgUserID, user.TgUserID)
	r.Equal(int64(0), user.AudioMaxSize)
	r.Equal(int64(0), user.VideoMaxSize)
	r.Equal(int64(0), user.PlaylistMaxSize)
	// r.False(user.CreatedAt.IsZero())
	// r.False(user.LastEventAt.IsZero())

	// Manually delete user and check 404.
	_, err = db.Exec("delete from users where tg_user_id = $1", tgUserID)
	r.NoError(err)
	_, err = usersRepo.Get(tgUserID)
	r.Error(err)
	r.True(errors.Is(err, sql.ErrNoRows))
}

func TestMediaRepository(t *testing.T) {
	r := require.New(t)

	db, err := repo.OpenDBAndMigrate("", repo.ModeMemory)
	r.NoError(err)
	defer func() {
		if err = db.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close database")
		}
	}()
	usersRepo, mediaRepo := repo.New(db)

	user, err := usersRepo.Create(tgUserID)
	r.NoError(err)

	multimedia := getMultimedia(r, db, user.ID)
	r.NoError(err)
	r.NotNil(multimedia)
	r.Empty(multimedia)

	media, err := mediaRepo.Create(user.ID, tgMessageID, uri, types.AudioMediaType)
	r.NoError(err)
	r.Equal(int64(1), media.ID)
	// r.NotEmpty(media.TgMessageID)
	r.NotEmpty(media.URI)
	// r.Equal(user.ID, media.UserID)
	// r.Equal(types.PendingMediaState, media.State)
	// r.Equal(types.AudioMediaType, media.Type)
	// r.False(media.CreatedAt.IsZero())
	// r.False(media.UpdatedAt.IsZero())
	// r.Nil(media.DoneAt)

	media = getMedia(r, db, media.ID, user.ID)
	r.Equal(int64(1), media.ID)
	// r.Equal(user.ID, media.UserID)
	// r.NotEmpty(media.TgMessageID)
	r.NotEmpty(media.URI)
	// r.Equal(types.PendingMediaState, media.State)
	// r.Equal(types.AudioMediaType, media.Type)
	// r.False(media.CreatedAt.IsZero())
	// r.False(media.UpdatedAt.IsZero())
	// r.Nil(media.DoneAt)

	// Check states.
	err = mediaRepo.UpdateState(media.ID, types.ErrorMediaState)
	r.NoError(err)
	media = getMedia(r, db, media.ID, user.ID)
	// r.Equal(types.ErrorMediaState, media.State)
	err = mediaRepo.UpdateState(media.ID, types.DoneMediaState)
	r.NoError(err)
	media = getMedia(r, db, media.ID, user.ID)
	// r.Equal(types.DoneMediaState, media.State)
	// Check incorrect state.
	err = mediaRepo.UpdateState(media.ID, "asd")
	r.Error(err)
	r.Contains(err.Error(), "CHECK constraint failed")

	media, err = mediaRepo.Create(user.ID, tgMessageID, uri, types.VideoMediaType)
	r.NoError(err)
	r.Equal(int64(2), media.ID)
	// r.Equal(types.VideoMediaType, media.Type)
	media = getMedia(r, db, media.ID, user.ID)
	r.Equal(int64(2), media.ID)
	// r.Equal(types.VideoMediaType, media.Type)

	multimedia = getMultimedia(r, db, user.ID)
	r.NoError(err)
	r.NotNil(multimedia)
	r.Len(multimedia, 2)

	media, err = mediaRepo.Create(user.ID, tgMessageID, uri, types.VideoMediaType)
	r.NoError(err)
	err = mediaRepo.UpdateState(media.ID, types.ErrorMediaState)
	r.NoError(err)

	title := "hello neighbor"
	err = mediaRepo.UpdateTitle(media.ID, title)
	r.NoError(err)
	media = getMedia(r, db, media.ID, user.ID)
	r.Equal(title, media.Title)

	// Check get pending multimedia.
	multimedia, err = mediaRepo.GetInProgress(user.ID)
	r.NoError(err)
	r.NotNil(multimedia)
	//nolint:godox // will resolve it later
	// todo: when error state will be like "pending" check that length is 2
	r.Len(multimedia, 1)
	// r.Equal(types.PendingMediaState, multimedia[0].State)
	//nolint:godox // will resolve it later
	// todo: when error state will be like "pending" check that
	// r.Equal(errorMediaState, multimedia[1].State)

	// Check that doneAt is not zero.
	_, err = db.Exec("update media set done_at = current_timestamp where id = $1", media.ID)
	r.NoError(err)
	media = getMedia(r, db, media.ID, user.ID)
	// r.NotNil(media.DoneAt)
	// r.False(media.DoneAt.IsZero())

	// Check create with incorrect media type.
	media, err = mediaRepo.Create(user.ID, tgMessageID, uri, "asd")
	r.Error(err)
	r.Contains(err.Error(), "CHECK constraint failed")
	r.Nil(media)

	// Check incorrect user id.
	media, err = mediaRepo.Create(123, tgMessageID, uri, types.AudioMediaType)
	r.Error(err)
	r.Contains(err.Error(), "FOREIGN KEY constraint failed")
	r.Nil(media)

	// Check delete cascade.
	_, err = db.Exec("delete from users where id = $1", user.ID)
	r.NoError(err)
	multimedia = getMultimedia(r, db, user.ID)
	r.NoError(err)
	r.NotNil(multimedia)
	r.Len(multimedia, 0)
}

func getMedia(r *require.Assertions, db *sqlx.DB, id, userID int64) *types.Media {
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

func getMultimedia(r *require.Assertions, db *sqlx.DB, userID int64) []*types.Media {
	multimedia := make([]*types.Media, 0)
	err := db.Select(&multimedia, "select * from media where user_id = $1", userID)
	r.NoError(err)
	r.NotNil(multimedia)
	return multimedia
}
