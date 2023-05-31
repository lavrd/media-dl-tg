package types

import (
	"errors"
	"time"
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

type MediaType string

const (
	AudioMediaType MediaType = "audio"
	VideoMediaType MediaType = "video"
)

type Entity string

const (
	EntityMedia    Entity = "media"
	EntityPlaylist Entity = "playlist"
)

type MediaLink struct {
	URI  string
	Type MediaType
}

type User struct {
	CreatedAt       time.Time
	LastEventAt     time.Time
	ID              int64
	TgUserID        int64
	AudioMaxSize    int64
	VideoMaxSize    int64
	PlaylistMaxSize int64
}

type Media struct {
	DoneAt      *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	URI         string
	Title       string
	State       MediaState
	Type        MediaType
	TgMessageID int
	ID          int64
	UserID      int64
}
