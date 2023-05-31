package plugin

import (
	"time"

	"media-dl-tg/internal/types"
)

type Meta struct {
	// Human URL to resource.
	URL   string
	Title string
	// Raw URL to download resource from.
	RawURL   string
	Size     int64 // in bytes
	Duration time.Duration
}

type Playlist struct {
	Media []Meta
}

func (p Playlist) Size() int64 { return int64(len(p.Media)) }

type Plugin interface {
	GetMeta() (*Meta, error)
	GetPlaylist() (*Playlist, error)
	ParseEntity(text string) (types.Entity, string, error)
}
