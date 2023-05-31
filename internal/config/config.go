package config

import (
	"fmt"
	"os"
	"strconv"

	"media-dl-tg/internal/types"
)

//nolint:govet // disable field aligment for better reading
type Config struct {
	Verbose               bool
	TgBotToken            string
	TgBotEndpoint         string
	MediaNPlaylistWorkers int
	ChunksWorkers         int
	DatabaseFilepath      string
	MediaFolder           string
}

func Read() (*Config, error) {
	cfg := &Config{}
	cfg.Verbose = os.Getenv("VERBOSE") == "1"
	cfg.TgBotToken = os.Getenv("TG_BOT_TOKEN")
	cfg.TgBotEndpoint = os.Getenv("TG_BOT_ENDPOINT")
	rawMediaNPlaylistWorkers := os.Getenv("MEDIA_N_PLAYLIST_WORKERS")
	if rawMediaNPlaylistWorkers == "" {
		rawMediaNPlaylistWorkers = "3"
	}
	mediaNPlaylistWorkers, err := strconv.Atoi(rawMediaNPlaylistWorkers)
	if err != nil {
		return nil, fmt.Errorf("failed to parse MEDIA_N_PLAYLIST_WORKERS env: %w", types.ErrInternal)
	}
	cfg.MediaNPlaylistWorkers = mediaNPlaylistWorkers
	rawChunksWorkers := os.Getenv("CHUNKS_WORKER")
	if rawChunksWorkers == "" {
		rawChunksWorkers = "100"
	}
	chunksWorkers, err := strconv.Atoi(rawChunksWorkers)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CHUNKS_WORKER env: %w", types.ErrInternal)
	}
	cfg.ChunksWorkers = chunksWorkers
	cfg.DatabaseFilepath = os.Getenv("DATABASE_FILEPATH")
	if cfg.DatabaseFilepath == "" {
		cfg.DatabaseFilepath = "media_dl_tg.db"
	}
	cfg.MediaFolder = os.Getenv("MEDIA_FOLDER")
	if cfg.MediaFolder == "" {
		cfg.MediaFolder = "media"
	}
	return cfg, nil
}
