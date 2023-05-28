package main

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

const (
	tgUserID    int64 = 249191443
	tgMessageID int   = 554555
	uri               = "https://example.com/media/213"
)

func TestUserRepository(t *testing.T) {
	r := require.New(t)

	db, err := openDBAndMigrate("", dbModeMemory)
	r.NoError(err)
	defer func() {
		if err = db.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close database")
		}
	}()
	repo := &userRepository{db: db}

	user, err := repo.create(tgUserID)
	r.NoError(err)
	r.Equal(int64(1), user.ID)
	r.Equal(tgUserID, user.TgUserID)
	r.Equal(int64(0), user.AudioMaxSize)
	r.Equal(int64(0), user.VideoMaxSize)
	r.Equal(int64(0), user.PlaylistMaxSize)
	r.False(user.CreatedAt.IsZero())
	r.False(user.LastEventAt.IsZero())

	// Check 409.
	_, err = repo.create(tgUserID)
	r.Error(err)
	r.Contains(err.Error(), "UNIQUE")

	user, err = repo.get(tgUserID)
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
	_, err = repo.get(tgUserID)
	r.Error(err)
	r.True(errors.Is(err, sql.ErrNoRows))
}

func TestMediaRepository(t *testing.T) {
	r := require.New(t)

	db, err := openDBAndMigrate("", dbModeMemory)
	r.NoError(err)
	defer func() {
		if err = db.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close database")
		}
	}()
	userRepo := &userRepository{db: db}
	mediaRepo := &mediaRepository{db: db}

	user, err := userRepo.create(tgUserID)
	r.NoError(err)

	multimedia := getMultimedia(r, mediaRepo, user.ID)
	r.NoError(err)
	r.NotNil(multimedia)
	r.Empty(multimedia)

	media, err := mediaRepo.create(user.ID, tgMessageID, uri, audioMediaType)
	r.NoError(err)
	r.Equal(int64(1), media.ID)
	r.NotEmpty(media.TgMessageID)
	r.NotEmpty(media.URI)
	r.Equal(user.ID, media.UserID)
	r.Equal(pendingMediaState, media.State)
	r.Equal(audioMediaType, media.MediaType)
	r.False(media.CreatedAt.IsZero())
	r.False(media.UpdatedAt.IsZero())
	r.Nil(media.DoneAt)

	media = getMedia(r, mediaRepo, media.ID, user.ID)
	r.Equal(int64(1), media.ID)
	r.Equal(user.ID, media.UserID)
	r.NotEmpty(media.TgMessageID)
	r.NotEmpty(media.URI)
	r.Equal(pendingMediaState, media.State)
	r.Equal(audioMediaType, media.MediaType)
	r.False(media.CreatedAt.IsZero())
	r.False(media.UpdatedAt.IsZero())
	r.Nil(media.DoneAt)

	// Check states.
	err = mediaRepo.updateState(media.ID, errorMediaState)
	r.NoError(err)
	media = getMedia(r, mediaRepo, media.ID, user.ID)
	r.Equal(errorMediaState, media.State)
	err = mediaRepo.updateState(media.ID, doneMediaState)
	r.NoError(err)
	media = getMedia(r, mediaRepo, media.ID, user.ID)
	r.Equal(doneMediaState, media.State)
	// Check incorrect state.
	err = mediaRepo.updateState(media.ID, "asd")
	r.Error(err)
	r.Contains(err.Error(), "CHECK constraint failed")

	media, err = mediaRepo.create(user.ID, tgMessageID, uri, videoMediaType)
	r.NoError(err)
	r.Equal(int64(2), media.ID)
	r.Equal(videoMediaType, media.MediaType)
	media = getMedia(r, mediaRepo, media.ID, user.ID)
	r.Equal(int64(2), media.ID)
	r.Equal(videoMediaType, media.MediaType)

	multimedia = getMultimedia(r, mediaRepo, user.ID)
	r.NoError(err)
	r.NotNil(multimedia)
	r.Len(multimedia, 2)

	media, err = mediaRepo.create(user.ID, tgMessageID, uri, videoMediaType)
	r.NoError(err)
	err = mediaRepo.updateState(media.ID, errorMediaState)
	r.NoError(err)

	title := "hello neighbor"
	err = mediaRepo.updateTitle(media.ID, title)
	r.NoError(err)
	media = getMedia(r, mediaRepo, media.ID, user.ID)
	r.Equal(title, media.Title)

	// Check get pending multimedia.
	multimedia, err = mediaRepo.getInProgress(user.ID)
	r.NoError(err)
	r.NotNil(multimedia)
	//nolint:godox // will resolve it later
	// todo: when error state will be like "pending" check that length is 2
	r.Len(multimedia, 1)
	r.Equal(pendingMediaState, multimedia[0].State)
	//nolint:godox // will resolve it later
	// todo: when error state will be like "pending" check that
	// r.Equal(errorMediaState, multimedia[1].State)

	// Check that doneAt is not zero.
	_, err = db.Exec("update media set done_at = current_timestamp where id = $1", media.ID)
	r.NoError(err)
	media = getMedia(r, mediaRepo, media.ID, user.ID)
	r.NotNil(media.DoneAt)
	r.False(media.DoneAt.IsZero())

	// Check create with incorrect media type.
	media, err = mediaRepo.create(user.ID, tgMessageID, uri, "asd")
	r.Error(err)
	r.Contains(err.Error(), "CHECK constraint failed")
	r.Nil(media)

	// Check incorrect user id.
	media, err = mediaRepo.create(123, tgMessageID, uri, audioMediaType)
	r.Error(err)
	r.Contains(err.Error(), "FOREIGN KEY constraint failed")
	r.Nil(media)

	// Check delete cascade.
	_, err = db.Exec("delete from users where id = $1", user.ID)
	r.NoError(err)
	multimedia = getMultimedia(r, mediaRepo, user.ID)
	r.NoError(err)
	r.NotNil(multimedia)
	r.Len(multimedia, 0)
}

func getMedia(r *require.Assertions, mediaRepo *mediaRepository, id, userID int64) *media {
	multimedia := getMultimedia(r, mediaRepo, userID)
	r.NotEmpty(multimedia)
	for _, media := range multimedia {
		if media.ID == id {
			return media
		}
	}
	r.Fail("media is not found")
	return nil
}

func getMultimedia(r *require.Assertions, mediaRepo *mediaRepository, userID int64) []*media {
	multimedia := make([]*media, 0)
	err := mediaRepo.db.Select(&multimedia, "select * from media where user_id = $1", userID)
	r.NoError(err)
	r.NotNil(multimedia)
	return multimedia
}
