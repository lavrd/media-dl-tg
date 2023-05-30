package plugin

import (
	"time"

	"media-dl-tg/internal/types"
)

// todo: describe all fields and why we need them
type Meta struct {
	ID       string
	Title    string
	Duration time.Duration
	Size     int64
	RawURL   string
}

type Playlist struct {
	Media []Meta
}

func (p Playlist) Size() int64 { return int64(len(p.Media)) }

// todo: describe all functions and why we need them
type Plugin interface {
	GetMeta() (*Meta, error)
	GetPlaylist() (*Playlist, error)
	ParseEntity(text string) (types.Entity, string, error)
}
