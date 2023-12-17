package types

import (
	"errors"
	"time"

	internal_plugin "github.com/lavrd/media-dl-tg/pkg/plugin"
)

var (
	ErrInternal     = errors.New("internal error")
	ErrSizeExceeded = errors.New("size is exceeded")
)

type MediaState string

const (
	PendingMediaState MediaState = "pending"
	ErrorMediaState   MediaState = "error"
	DoneMediaState    MediaState = "done"
)

type MediaLink struct {
	URI  string
	Type internal_plugin.MediaType
}

type User struct {
	ID              int64
	TgUserID        int64
	AudioMaxSize    int64
	VideoMaxSize    int64
	PlaylistMaxSize int64
	LastEventAt     time.Time
	CreatedAt       time.Time
}

type Media struct {
	ID          int64
	UserID      int64
	TgMessageID int
	URI         string
	Title       string
	State       MediaState
	Type        internal_plugin.MediaType
	DoneAt      *time.Time
	UpdatedAt   time.Time
	CreatedAt   time.Time
}
