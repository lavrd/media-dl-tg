package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/lavrd/media-dl-tg/internal/types"
)

type Config struct {
	Verbose    bool
	TgBotToken string
	// Endpoint to Telegram bot API server.
	TgBotEndpoint    string
	TgUpdatesTimeout int
	// How many workers should be started to download media and playlists.
	MediaNPlaylistWorkers int
	// How many workers should be started to download each media chunks.
	ChunksWorkers  int
	DatabaseFile   string
	MigrationsPath string
	// In this folder all media is stored.
	MediaFolder string
}

func Read() (*Config, error) {
	cfg := &Config{}
	cfg.Verbose = os.Getenv("VERBOSE") == "1"
	cfg.TgBotToken = os.Getenv("TG_BOT_TOKEN")
	cfg.TgBotEndpoint = os.Getenv("TG_BOT_ENDPOINT")
	rawTgUpdatesTimeout := os.Getenv("TG_UPDATES_TIMEOUT")
	if rawTgUpdatesTimeout == "" {
		rawTgUpdatesTimeout = "1"
	}
	tgUpdatesTimeout, err := strconv.Atoi(rawTgUpdatesTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to parse TG_UPDATES_TIMEOUT")
	}
	cfg.TgUpdatesTimeout = tgUpdatesTimeout
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
	cfg.DatabaseFile = os.Getenv("DATABASE_FILE")
	if cfg.DatabaseFile == "" {
		cfg.DatabaseFile = "media_dl_tg.db"
	}
	cfg.MigrationsPath = os.Getenv("MIGRATIONS_PATH")
	if cfg.MigrationsPath == "" {
		cfg.MigrationsPath = "file://./migrations"
	}
	cfg.MediaFolder = os.Getenv("MEDIA_FOLDER")
	if cfg.MediaFolder == "" {
		cfg.MediaFolder = "media"
	}
	return cfg, nil
}
