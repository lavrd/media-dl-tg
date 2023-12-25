package playlist

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/lavrd/media-dl-tg/internal/keeper"
	"github.com/lavrd/media-dl-tg/internal/repo"
	"github.com/lavrd/media-dl-tg/internal/task"
	"github.com/lavrd/media-dl-tg/internal/task/download/media"
	"github.com/lavrd/media-dl-tg/internal/types"
	internal_plugin "github.com/lavrd/media-dl-tg/pkg/plugin"
)

type Options struct {
	MediaLink     *types.MediaLink
	Plugin        internal_plugin.Plugin
	MaxSize       int64
	ChunksWorkers int
	MediaFolder   string
	UserID        int64
	MessageID     int
	Keeper        keeper.Keeper
	Handler       task.ResultHandler
	TaskC         task.Task
	DoneC         chan struct{}
}

type Task struct {
	opts *Options
}

func New(opts *Options) *Task { return &Task{opts: opts} }

func (t *Task) Start() task.ResultData {
	mediaID, err := t.opts.Keeper.CreatePlaylist(
		context.Background(), t.opts.UserID, t.opts.MessageID, t.opts.MediaLink.URI, t.opts.MediaLink.Type)
	if err != nil {
		return task.ResultData{Err: fmt.Errorf("failed to create media for user: %w", err)}
	}
	if t.opts.MaxSize == 0 {
		return task.ResultData{Err: types.ErrSizeExceeded}
	}
	err = t.download()
	return task.ResultData{
		MediaID: mediaID,
		Err:     err,
	}
}

func (t *Task) download() error {
	playlist, err := t.opts.Plugin.GetPlaylist(context.Background(), t.opts.MediaLink.URI)
	if err != nil {
		return fmt.Errorf("failed to get playlist info from plugin: %w", err)
	}
	if t.opts.MaxSize < playlist.Size() {
		return fmt.Errorf("sent playlist size is more than allowed: %w", types.ErrSizeExceeded)
	}
	for _, entry := range playlist.URLs {
		mediaLink := &types.MediaLink{URI: entry, Type: t.opts.MediaLink.Type}
		t.opts.TaskC <- media.New(
			t.opts.Plugin, mediaLink, t.opts.MediaFolder, t.opts.ChunksWorkers, t.opts.DoneC)
	}

	return nil
}

type Handler struct {
	mediaRepo repo.MediaRepository
}

func NewHandler(mediaRepo repo.MediaRepository) *Handler {
	return &Handler{mediaRepo: mediaRepo}
}

func (h *Handler) Handle(data task.ResultData) {
	if err := h.mediaRepo.UpdateState(context.Background(), data.MediaID, types.DoneMediaState); err != nil {
		log.Error().Err(err).Int64("media_id", data.MediaID).Msg("failed to set playlist as done")
	}
}
