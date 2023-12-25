package chunk

import (
	"fmt"
)

type downloadChunkTask struct {
	downloader *downloader
	// Wait group to wait until all started download chunk tasks will be finished.
	wg *atomic.Int64
	// Using it to stop waiting for completed previous chunks as they can be in error state.
	stopC chan struct{}
	// Notify about error.
	errC chan error
	// Current chunk number.
	num int64
}

func newDownloadChunkTask(
	downloader *downloader, num int64, wg *atomic.Int64, stopC chan struct{}, errC chan error,
) *downloadChunkTask {

	return &downloadChunkTask{
		downloader: downloader,
		num:        num,
		wg:         wg,
		stopC:      stopC,
		errC:       errC,
	}
}

func (t *downloadChunkTask) Do() {
	defer t.wg.Add(-1)
	t.wg.Add(1)
	if err := t.downloader.downloadChunk(t.num, t.stopC); err != nil {
		t.errC <- fmt.Errorf("failed to download %d chunk: %w", t.num, err)
		return
	}
	t.errC <- nil
}
