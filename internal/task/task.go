package task

import (
	"github.com/rs/zerolog/log"
)

type ResultData struct {
	MediaID int64
	Err     error
}

type ResultHandler interface {
	Handle(data ResultData)
}

type Task interface {
	Start() (ResultData, error)
	Handler() ResultHandler
}

type Worker struct {
	taskC chan Task
}

func (w *Worker) Run() {
	for task := range w.taskC {
		data, err := task.Start()
		if err != nil {
			log.Error().Err(err).Msg("failed to start task")
			continue
		}
		handler := task.Handler()
		handler.Handle(data)
	}
}

func RunWorkers(taskC chan Task, count int) {
	for i := 0; i < count; i++ {
		go (&Worker{taskC: taskC}).Run()
	}
}
