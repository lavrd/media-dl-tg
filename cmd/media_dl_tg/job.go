package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

type Job interface {
	Do() error
}

func StartJob(job Job, interval time.Duration, doneC chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		for {
			select {
			case <-ticker.C:
				if err := job.Do(); err != nil {
					log.Error().Err(err).Msg("failed to do job")
				}
			case <-doneC:
				ticker.Stop()
				return
			}
		}
	}()
}

// CleanJob is a job which delete media which has 1h and more lifetime.
type CleanJob struct {
	mediaFolder string
}

func (w *CleanJob) Do() error {
	if err := filepath.Walk(w.mediaFolder, w.checkFile); err != nil {
		return fmt.Errorf("failed to walk through media folder: %w", err)
	}
	return nil
}

func (w *CleanJob) checkFile(path string, info fs.FileInfo, err error) error {
	if err != nil {
		return fmt.Errorf("failed to get file: %w", err)
	}
	if info.IsDir() {
		return nil
	}
	logger := log.With().
		Str("file_name", info.Name()).Str("last_modified_time", info.ModTime().String()).
		Logger()
	// Don't delete file if last modification was less than a half hour ago.
	if time.Since(info.ModTime()) < time.Minute*10 {
		logger.Debug().Msg("file is fresh")
		return nil
	}
	// Delete if last file modification was more than a half hour ago.
	if err := os.RemoveAll(path); err != nil {
		logger.Error().Err(err).Msg("failed to remove file")
		return nil
	}
	logger.Debug().Msg("file successfully deleted")
	return nil
}
