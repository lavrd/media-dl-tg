package main

import (
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/lavrd/media-dl-tg/internal/types"
	internal_plugin "github.com/lavrd/media-dl-tg/pkg/plugin"
)

func Test(t *testing.T) {
	r := require.New(t)

	log.Logger = log.
		Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().Caller().Logger().
		Level(zerolog.InfoLevel)

	plugin, err := internal_plugin.Open("../../plugin.so")
	r.NoError(err)

	const workers = 10
	taskC := make(chan task)
	runWorkers(taskC, workers)
	mediaLink := &types.MediaLink{URI: "", Type: internal_plugin.VideoMediaType}
	taskC <- newDownloadMediaTask(plugin, mediaLink, "./", 10, nil)
}
