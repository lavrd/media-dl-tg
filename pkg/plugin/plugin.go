package plugin

import (
	"context"
	"errors"
	"fmt"
	"plugin"
	"time"
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

var ErrBadSignature = errors.New("bad function signature")

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
	URLs []string
}

func (p Playlist) Size() int64 { return int64(len(p.URLs)) }

type Plugin interface {
	GetMeta(ctx context.Context, uri string, mediaType MediaType) (*Meta, error)
	GetPlaylist(ctx context.Context, uri string) (*Playlist, error)
	ParseEntity(text string) (Entity, string, error)
}

func Open(path string) (Plugin, error) {
	stdPlug, err := plugin.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin file: %w", err)
	}
	rawNewF, err := stdPlug.Lookup("New")
	if err != nil {
		return nil, fmt.Errorf("failed to find init function: %w", err)
	}
	newF, _ := rawNewF.(func() (Plugin, error))
	// if !ok {
	// 	return nil, fmt.Errorf("init function is not properly configured: %w", ErrBadSignature)
	// }
	plug, err := newF()
	if err != nil {
		return nil, fmt.Errorf("failed to init plugin: %w", err)
	}
	return plug, nil
}
