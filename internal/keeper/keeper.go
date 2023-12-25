package keeper

import (
	"context"

	internal_plugin "github.com/lavrd/media-dl-tg/pkg/plugin"
)

type Keeper interface {
	CreateMedia(ctx context.Context, userID int64, messageID int, mediaURI string, mediaType internal_plugin.MediaType) (int64, error)
}
