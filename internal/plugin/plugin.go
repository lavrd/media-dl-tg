package plugin

import (
	"fmt"
	"plugin"
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

func Open(path string) (Plugin, error) {
	stdPlug, err := plugin.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin file: %w", err)
	}
	symb, err := stdPlug.Lookup("Init")
	if err != nil {
		return nil, fmt.Errorf("failed to init plugin: %w", err)
	}
	plug, ok := symb.(Plugin)
	if !ok {
		return nil, fmt.Errorf("plugin is not suit for our interface: %w", types.ErrInternal)
	}
	return plug, nil
}
