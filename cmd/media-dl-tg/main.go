package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"media-dl-tg/internal/config"
	"media-dl-tg/internal/plugin"
	"media-dl-tg/internal/repo"
	"media-dl-tg/internal/types"
)

const (
	maxTgAPIFileSize = 50 * 1024 * 1024 // 50mb

	topicChooseMediaType = "cmt"

	cbDelimiter       = "@"
	cbCMTValDelimiter = ":"
)

const (
	minChunkSize int64 = 1024 * 100 // 100kb
	maxChunkSize int64 = 1024 * 200 // 200kb
)

func main() {
	log.Logger = log.
		Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().Caller().Logger().
		Level(zerolog.InfoLevel)

	cfg, err := config.Read()
	if err != nil {
		fmt.Println("failed to read config:", err)
		os.Exit(1)
	}

	if cfg.Verbose {
		log.Logger = log.Level(zerolog.TraceLevel)
	}

	// We don't need to keep files in media folder after restart because we can't use them.
	if err = os.RemoveAll(cfg.MediaFolder); err != nil {
		log.Fatal().Err(err).Msg("failed to delete media folder")
	}
	if err = os.Mkdir(cfg.MediaFolder, os.ModePerm); err != nil {
		log.Fatal().Err(err).Msg("failed to create folder")
	}

	db, err := repo.OpenDBAndMigrate(cfg.DatabaseFilepath, repo.ModeRWC)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open database and do migrations")
	}
	usersRepo, mediaRepo := repo.New(db)
	err = mediaRepo.DeleteInProgress(context.TODO())
	if err != nil {
		log.Fatal().Err(err).Msg("failed to delete media in progress")
	}

	doneC := make(chan struct{})
	startJob(&cleanJob{}, time.Hour, doneC)
	bot, err := newBot(cfg, usersRepo, mediaRepo, doneC)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize telegram bot")
	}

	interruptC := make(chan os.Signal, 1)
	defer close(interruptC)
	signal.Notify(interruptC, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	<-interruptC
	log.Debug().Msg("handle SIGINT, SIGQUIT, SIGTERM")

	// Stop receiving updates.
	bot.stop()
	// Stop all downloads and other routines.
	close(doneC)
	// Close connection with database and other stuff.
	bot.close()
	if err := db.Close(); err != nil {
		log.Error().Err(err).Msg("failed to close database connection")
	}
	log.Info().Msg("bot has been stopped")
}

type bot struct {
	tg       *tgbotapi.BotAPI
	updatesC tgbotapi.UpdatesChannel

	usersRepo repo.UsersRepository
	mediaRepo repo.MediaRepository

	taskC chan task
	doneC chan struct{}

	plugin plugin.Plugin

	chunksWorkers int
}

func newBot(
	cfg *config.Config,
	usersRepo repo.UsersRepository, mediaRepo repo.MediaRepository,
	doneC chan struct{},
) (*bot, error) {

	// Create telegram bot client.
	tg, err := tgbotapi.NewBotAPI(cfg.TgBotToken)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize new telegram client: %w", err)
	}
	tgBotEndpoint := cfg.TgBotEndpoint
	if tgBotEndpoint != "" {
		tg.SetAPIEndpoint(tgBotEndpoint)
	}

	// Channel to send new task from Telegram user.
	taskC := make(chan task)
	runWorkers(taskC, cfg.MediaNPlaylistWorkers)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updatesC := tg.GetUpdatesChan(u)
	// Wait for updates and clear them if you don't want to handle a large backlog of old messages.
	time.Sleep(time.Second)
	updatesC.Clear()

	bot := &bot{
		tg:       tg,
		updatesC: updatesC,

		usersRepo: usersRepo,
		mediaRepo: mediaRepo,

		taskC: taskC,
		doneC: doneC,

		chunksWorkers: cfg.ChunksWorkers,
	}
	go bot.handleMessages()

	return bot, nil
}

func (b *bot) stop() {
	b.tg.StopReceivingUpdates()
	b.updatesC.Clear()
}

func (b *bot) close() {
	close(b.taskC)
}

func (b *bot) handleMessages() {
	log.Info().Msg("bot has started and waiting for updates")
	for {
		select {
		case update := <-b.updatesC:
			tgUser := update.SentFrom()
			chat := update.FromChat()
			user, err := b.checkUser(tgUser.ID, chat.ID)
			if err != nil {
				log.Error().Err(err).Msg("failed to check user")
				send500(b.tg, chat.ID)
				continue
			}
			// It means we don't need to continue flow.
			// For example it was start command and we just created user.
			if user == nil {
				continue
			}
			switch {
			case update.Message != nil:
				b.handleMessage(update.Message, user)
			case update.CallbackQuery != nil:
				b.handleCallback(update.CallbackQuery, user, b.doneC)
			}
		case <-b.doneC:
			return
		}
	}
}

func (b *bot) handleMessage(message *tgbotapi.Message, user *types.User) {
	logger := log.With().Int64("user_id", message.From.ID).Logger()

	if message.IsCommand() {
		b.handleCommand(message, user)
		return
	}

	entityType, entityID, err := b.plugin.ParseEntity(message.Text)
	if err != nil {
		reply(b.tg, message, "Link to the media or playlist is incorrect")
		return
	}
	switch entityType {
	case types.EntityMedia:
	case types.EntityPlaylist:
		if user.PlaylistMaxSize == 0 {
			reply(b.tg, message, "You are not allowed to download playlists")
			return
		}
	default:
		log.Error().Str("entity_type", string(entityType)).Msg("unknown entity type")
		reply500(b.tg, message)
		return
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, "Choose type: audio or video. "+
		"Most likely the audio will have better sound quality and a much smaller file size")
	msg.ReplyToMessageID = message.MessageID
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Video",
				prepCbData(
					topicChooseMediaType, prepareCMTCbValue(entityType, types.VideoMediaType, entityID))),
			tgbotapi.NewInlineKeyboardButtonData("Audio",
				prepCbData(
					topicChooseMediaType, prepareCMTCbValue(entityType, types.AudioMediaType, entityID))),
		),
	)
	if _, err := b.tg.Send(msg); err != nil {
		logger.Error().Err(err).Msg("failed to send message")
	}
}

func (b *bot) handleCommand(message *tgbotapi.Message, user *types.User) {
	switch message.Command() {
	case "me":
		reply(b.tg, message, strconv.Itoa(int(message.From.ID)))
	case "pending":
		media, err := b.mediaRepo.GetInProgress(context.TODO(), user.ID)
		if err != nil {
			log.Error().Err(err).Msg("failed to get media in progress")
			reply500(b.tg, message)
			return
		}
		if len(media) == 0 {
			reply(b.tg, message, "You don't have media in progress")
			return
		}
		text := "Media in progress"
		for _, entity := range media {
			name := entity.Title
			if name == "" {
				name = entity.URI
			}
			text = fmt.Sprintf("%s\n%s", text, name)
		}
		reply(b.tg, message, text)
	case "help":
		reply(b.tg, message, "Sorry, I am too lazy to write it.")
	}
}

// Return user as nil if we don't need to continue flow; for example if user is banned.
func (b *bot) checkUser(tgUserID, chatID int64) (*types.User, error) {
	logger := log.With().Int64("tg_user_id", tgUserID).Logger()
	user, err := b.usersRepo.Get(context.TODO(), tgUserID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		logger.Debug().Msg("new user")
		if _, err := b.usersRepo.Create(context.TODO(), tgUserID); err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		send(b.tg, chatID, "Send link to start downloading")
		return nil, nil
	}
	if user.AudioMaxSize == 0 && user.VideoMaxSize == 0 {
		send(b.tg, chatID, "You are not allowed to download media")
		return nil, nil
	}
	return user, nil
}

func (b *bot) handleCallback(callback *tgbotapi.CallbackQuery, user *types.User, doneC chan struct{}) {
	topic, value := parseCbData(callback.Data)
	switch topic {
	case topicChooseMediaType:
		b.handleChooseMediaTypeCallback(callback.Message, value, user, doneC)
		removeMarkup(b.tg, callback.Message.Chat.ID, callback.Message.MessageID)
	default:
		log.Error().Str("topic", topic).Msg("unknown topic")
		reply500(b.tg, callback.Message)
	}
}

func (b *bot) handleChooseMediaTypeCallback(
	message *tgbotapi.Message, value string, user *types.User, doneC chan struct{},
) {

	entityType, mediaTyp, uri := parseCMTCbValue(value)
	mediaLink := &types.MediaLink{URI: uri, Type: mediaTyp}

	/*
		Following code protects from multiple simultaneously downloads by one user.
	*/
	multimedia, err := b.mediaRepo.GetInProgress(context.TODO(), user.ID)
	if err != nil {
		reply500(b.tg, message)
		return
	}
	if len(multimedia) != 0 {
		reply(b.tg, message, "You already have media which are in progress")
		return
	}
	media, err := b.mediaRepo.Create(context.TODO(), user.ID, message.MessageID, mediaLink.URI, mediaLink.Type)
	if err != nil {
		reply500(b.tg, message)
		return
	}

	var downloadTask task
	switch entityType {
	case types.EntityPlaylist:
		downloadTask = newDownloadPlaylistTask(
			b.tg, message, user, media.ID, mediaLink, b.mediaRepo, b.taskC, b.chunksWorkers, doneC)
	case types.EntityMedia:
		downloadTask = newDownloadMediaTask(b.tg, message, user, media, mediaLink, b.mediaRepo, b.chunksWorkers, doneC)
	default:
		log.Error().Str("value", value).Msg("unknown entity type for cmt callback")
		return
	}

	select {
	case b.taskC <- downloadTask:
		reply(b.tg, message, fmt.Sprintf("Bot has started to download your %s and will send to you ASAP", entityType))
	default:
		reply(b.tg, message, "All workers are busy, try again later")
	}
}

type chunkManager struct {
	mu     *sync.Mutex
	chunks map[int64]*chunk
}

func newChunkManager(totalN int64) *chunkManager {
	manager := &chunkManager{
		mu:     &sync.Mutex{},
		chunks: make(map[int64]*chunk),
	}

	remain := totalN
	var i int64
	for {
		manager.prepareChunk(i, &remain)
		if remain == 0 {
			break
		}
		i++
	}

	return manager
}

func (m *chunkManager) prepareChunk(n int64, remain *int64) {
	size := getRandomChunkSize()
	if size >= *remain {
		size = *remain
	}
	*remain -= size

	var rangeStart int64
	for i := 0; int64(i) < n; i++ {
		if prevChunk, ok := m.chunks[int64(i)]; ok {
			rangeStart += prevChunk.size
		}
	}
	rangeStop := rangeStart + size

	m.chunks[n] = &chunk{
		size:       size,
		rangeStart: rangeStart,
		rangeStop:  rangeStop,
	}
}

func (m *chunkManager) setReady(n int64) {
	m.mu.Lock()
	m.chunks[n].ready = true
	m.mu.Unlock()
}

func (m *chunkManager) isReady(n int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chunks[n].ready
}

type chunk struct {
	size       int64
	rangeStart int64
	rangeStop  int64
	// This flag indicates that chunk wrote buffer to file.
	ready bool
}

type mediaInfo struct {
	filepath string
	title    string
	quality  string
	size     int64
	duration time.Duration
}

type downloader struct {
	plugin      plugin.Plugin
	mediaRepo   repo.MediaRepository
	manager     *chunkManager
	file        *os.File
	meta        *plugin.Meta
	mediaFolder string
	// Number of workers to download chunks in parallel.
	chunksWorkers int
}

func (d *downloader) download(
	mediaLink *types.MediaLink, user *types.User, mediaID int64, doneC chan struct{},
) (*mediaInfo, error) {

	meta, err := d.plugin.GetMeta()
	if err != nil {
		return nil, fmt.Errorf("failed to get meta info from plugin: %w", err)
	}
	d.meta = meta

	if meta.Duration == 0 {
		return nil, fmt.Errorf("we don't support to download streams or media is empty: %w", types.ErrInternal)
	}

	if err = d.mediaRepo.UpdateTitle(context.TODO(), mediaID, meta.Title); err != nil {
		log.Error().Err(err).
			Int64("media_id", mediaID).Str("title", meta.Title).
			Msg("failed to update media title")
	}

	switch mediaLink.Type {
	case types.AudioMediaType:
		if meta.Size > user.AudioMaxSize {
			return nil, fmt.Errorf("audio size exceeded: %d: %w", meta.Size, types.ErrSizeExceeded)
		}
	case types.VideoMediaType:
		if meta.Size > user.VideoMaxSize {
			return nil, fmt.Errorf("video size exceeded: %d: %w", meta.Size, types.ErrSizeExceeded)
		}
	default:
		return nil, fmt.Errorf("unknown media type: %s: %w", mediaLink.Type, types.ErrInternal)
	}

	//nolint:gosec // it is ok to have weak generator
	id := rand.Uint64()
	path := strings.NewReplacer(" ", "_", "/", "_").Replace(meta.Title)
	path = fmt.Sprintf("%s-%d.mp4", path, id)
	file, err := os.Create(fmt.Sprintf("%s/%s", d.mediaFolder, path))
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close file")
		}
	}()
	d.file = file

	log.Info().Str("title", meta.Title).Msg("started to download")

	manager := newChunkManager(meta.Size)
	d.manager = manager

	taskC := make(chan task)
	stopC := make(chan struct{})
	errC := make(chan error)
	defer close(errC)
	wg := &atomic.Int64{}

	runWorkers(taskC, d.chunksWorkers)

	go func() {
		defer close(taskC)
		for i := 0; i < len(manager.chunks); i++ {
			task := newDownloadChunkTask(d, int64(i), wg, stopC, errC)
			select {
			case taskC <- task:
			case <-stopC:
				return
			case <-doneC:
				return
			}
		}
		<-stopC
	}()

	var downloadChunkErr error
	defer func() {
		close(stopC)
		if downloadChunkErr != nil {
			// Here we are waiting for already started chunks downloads competition.
			// When all download chunk jobs return response we can leave this functions.
			for wg.Load() != 0 {
				select {
				case <-errC:
				case <-doneC:
				}
			}
		}
	}()

	for i := 0; i < len(manager.chunks); i++ {
		select {
		case err := <-errC:
			if err != nil {
				downloadChunkErr = err
				return nil, fmt.Errorf("failed to download chunk: %w", err)
			}
		case <-doneC:
			// As we need to stop our app we need to wait until all operations will be done.
			downloadChunkErr = fmt.Errorf("wait for goroutines: %w", types.ErrInternal)
			return nil, fmt.Errorf("media downloading was interrupted by done channel: %w", types.ErrInternal)
		}
	}

	return &mediaInfo{
		filepath: path,
		title:    meta.Title,
		// quality:  format.QualityLabel,
		size:     meta.Size,
		duration: meta.Duration,
	}, nil
}

func (d *downloader) downloadChunk(n int64, stopC chan struct{}) error {
	chunk := d.manager.chunks[n]

	buf, elapsed, err := d.requestChunk(chunk)
	if err != nil {
		return fmt.Errorf("failed to request chunk: %w", err)
	}

	tries := 0
	// Waiting for previous chunks. They should complete writing to file.
tryAgain:
	minN := n - int64(d.chunksWorkers)
	if minN < 0 {
		minN = 0
	}
	countToCheck := n - minN
	for {
		tries++
		for i := 0; int64(i) < countToCheck; i++ {
			current := minN + int64(i)
			if !d.manager.isReady(current) {
				select {
				case <-stopC:
					// We need to check this channel and exit on it
					// not to wait for previous chunks which was exited with errors.
					return fmt.Errorf("interrupted by stop channel: %w", types.ErrInternal)
				default:
					// We have this check to prevent endless loop.
					if tries > 250000 {
						return fmt.Errorf("maximum retries exceeded: %w", types.ErrInternal)
					}
					time.Sleep(time.Millisecond * 50)
					goto tryAgain
				}
			}
		}
		break
	}

	// Write to file as previous chunks are ready.
	if _, err := io.CopyN(d.file, buf, chunk.size); err != nil {
		return fmt.Errorf("failed to create buffer to file: %w", err)
	}
	// Set chunk as ready.
	d.manager.setReady(n)

	d.printProgress(n, elapsed)
	return nil
}

func (d *downloader) requestChunk(chunk *chunk) (*bytes.Buffer, time.Duration, error) {
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, d.meta.RawURL, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create new request: %w", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.rangeStart, chunk.rangeStop))

	res, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to do http request: %w", err)
	}
	defer func() {
		if err = res.Body.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close response body")
		}
	}()

	// Download media chunk to temporary buffer.
	buf := bytes.NewBuffer([]byte{})
	start := time.Now()
	_, err = io.CopyN(buf, res.Body, chunk.size)
	elapsed := time.Since(start)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, fmt.Errorf("failed to create media chunk to buffer: %w", err)
	}
	return buf, elapsed, nil
}

func (d *downloader) printProgress(n int64, elapsed time.Duration) {
	currentN := n + 1
	chunkN := int64(len(d.manager.chunks))
	chunk := d.manager.chunks[n]
	if chunkN == currentN {
		log.Info().Str("title", d.meta.Title).Msg("successfully downloaded")
		return
	}
	progress := fmt.Sprintf("%.2f%%", float64(chunk.rangeStop)/float64(d.meta.Size)*100)
	speed := fmt.Sprintf("%.2fMb/s", float64(chunk.size)/elapsed.Seconds()/1024/1024)
	log.Trace().
		Str("title", d.meta.Title).
		Str("part", fmt.Sprintf("%d/%d", currentN, chunkN)).
		Str("progress", progress).
		Str("speed", speed).
		Msg("downloading")
}

type task interface {
	do()
}

type worker struct {
	taskC chan task
}

func (w *worker) run() {
	for task := range w.taskC {
		task.do()
	}
}

func runWorkers(taskC chan task, count int) {
	for i := 0; i < count; i++ {
		go (&worker{taskC: taskC}).run()
	}
}

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

func (t *downloadChunkTask) do() {
	defer t.wg.Add(-1)
	t.wg.Add(1)
	if err := t.downloader.downloadChunk(t.num, t.stopC); err != nil {
		t.errC <- fmt.Errorf("failed to download %d chunk: %w", t.num, err)
		return
	}
	t.errC <- nil
}

type downloadPlaylistTask struct {
	plugin        plugin.Plugin
	tg            *tgbotapi.BotAPI
	message       *tgbotapi.Message
	user          *types.User
	mediaLink     *types.MediaLink
	mediaRepo     repo.MediaRepository
	taskC         chan task
	doneC         chan struct{}
	chunksWorkers int
	mediaID       int64
}

func newDownloadPlaylistTask(
	tg *tgbotapi.BotAPI, message *tgbotapi.Message, user *types.User,
	mediaID int64, link *types.MediaLink, mediaRepo repo.MediaRepository,
	taskC chan task, chunksWorkers int, doneC chan struct{},
) *downloadPlaylistTask {

	return &downloadPlaylistTask{
		tg:            tg,
		message:       message,
		user:          user,
		mediaID:       mediaID,
		mediaLink:     link,
		mediaRepo:     mediaRepo,
		taskC:         taskC,
		chunksWorkers: chunksWorkers,
		doneC:         doneC,
	}
}

func (t *downloadPlaylistTask) do() {
	if t.user.PlaylistMaxSize == 0 {
		reply(t.tg, t.message, "You are not allowed to download playlists")
		return
	}
	err := t.download()
	switch {
	case err == nil:
	case errors.Is(err, types.ErrSizeExceeded):
		reply(t.tg, t.message, "Your playlist contains medias more than you are allowed to download")
	default:
		log.Error().Err(err).
			Str("playlist_link", t.mediaLink.URI).Str("media_type", string(t.mediaLink.Type)).
			Int64("chat_id", t.message.Chat.ID).Int("message_id", t.message.MessageID).
			Msg("failed to download playlist")
		reply500(t.tg, t.message)
	}
}

func (t *downloadPlaylistTask) download() error {
	playlist, err := t.plugin.GetPlaylist()
	if err != nil {
		return fmt.Errorf("failed to get playlist info from plugin: %w", err)
	}

	if t.user.PlaylistMaxSize < playlist.Size() {
		return fmt.Errorf("sent playlist size is more than allowed: %w", types.ErrSizeExceeded)
	}
	for _, entry := range playlist.Media {
		media, err := t.mediaRepo.Create(context.TODO(), t.user.ID, t.message.MessageID, t.mediaLink.URI, t.mediaLink.Type)
		if err != nil {
			log.Error().Err(err).Int64("user_id", t.user.ID).Msg("failed to create new media from playlist")
			continue
		}
		mediaLink := &types.MediaLink{URI: entry.URL, Type: t.mediaLink.Type}
		t.taskC <- newDownloadMediaTask(t.tg, t.message, t.user, media, mediaLink, t.mediaRepo, t.chunksWorkers, t.doneC)
	}
	if err := t.mediaRepo.UpdateState(context.TODO(), t.mediaID, types.DoneMediaState); err != nil {
		log.Error().Err(err).Int64("media_id", t.mediaID).Msg("failed to set playlist as done")
	}
	return nil
}

type downloadMediaTask struct {
	mediaRepo     repo.MediaRepository
	tg            *tgbotapi.BotAPI
	message       *tgbotapi.Message
	user          *types.User
	media         *types.Media
	mediaLink     *types.MediaLink
	doneC         chan struct{}
	mediaFolder   string
	chunksWorkers int
}

func newDownloadMediaTask(
	tg *tgbotapi.BotAPI, message *tgbotapi.Message, user *types.User,
	media *types.Media, mediaLink *types.MediaLink, mediaRepo repo.MediaRepository,
	chunksWorker int, doneC chan struct{},
) *downloadMediaTask {

	return &downloadMediaTask{
		tg:            tg,
		message:       message,
		user:          user,
		media:         media,
		mediaLink:     mediaLink,
		mediaRepo:     mediaRepo,
		doneC:         doneC,
		chunksWorkers: chunksWorker,
	}
}

func (t *downloadMediaTask) do() {
	err := t.download()
	switch {
	case err == nil:
	case errors.Is(err, types.ErrSizeExceeded):
		reply(t.tg, t.message, "Media size is more than you allowed to download")
		if err = t.mediaRepo.UpdateState(context.TODO(), t.media.ID, types.DoneMediaState); err != nil {
			log.Error().Err(err).Int64("media_id", t.media.ID).Msg("failed to update state")
		}
	default:
		log.Error().Err(err).
			Int64("media_id", t.media.ID).Str("media_link", t.mediaLink.URI).Str("media_type", string(t.mediaLink.Type)).
			Int64("chat_id", t.message.Chat.ID).Int("message_id", t.message.MessageID).
			Msg("failed to download media")
		if err := t.mediaRepo.UpdateState(context.TODO(), t.media.ID, types.ErrorMediaState); err != nil {
			log.Error().Err(err).Int64("media_id", t.media.ID).Msg("failed to update media state to error")
		}
		reply500(t.tg, t.message)
	}
}

func (t *downloadMediaTask) download() error {
	logger := log.With().
		Int64("chat_id", t.message.Chat.ID).Int("message_id", t.message.MessageID).
		Int64("media_id", t.media.ID).Str("media_link", t.mediaLink.URI).Str("media_type", string(t.mediaLink.Type)).
		Logger()

	mediaInfo, err := (&downloader{
		mediaRepo:     t.mediaRepo,
		chunksWorkers: t.chunksWorkers,
	}).download(t.mediaLink, t.user, t.media.ID, t.doneC)
	if err != nil {
		return fmt.Errorf("failed to download media: %w", err)
	}

	logger = logger.With().
		Str("media_title", mediaInfo.title).Str("media_filepath", mediaInfo.filepath).
		Int64("media_size", mediaInfo.size).
		Logger()

	if mediaInfo.size > maxTgAPIFileSize {
		logger.Warn().
			Msg("be aware that media size is more than 50mb, and you need to use custom (local) telegram bot api server")
	}

	fileData := tgbotapi.FilePath(fmt.Sprintf("%s/%s", t.mediaFolder, mediaInfo.filepath))
	duration := int(math.Floor(mediaInfo.duration.Seconds()))

	var chattable tgbotapi.Chattable
	switch t.mediaLink.Type {
	case types.AudioMediaType:
		video := tgbotapi.NewVideo(t.message.Chat.ID, fileData)
		video.ReplyToMessageID = t.message.MessageID
		video.Caption = fmt.Sprintf("%s - %s", mediaInfo.title, mediaInfo.quality)
		video.Duration = duration
		chattable = video
	case types.VideoMediaType:
		audio := tgbotapi.NewAudio(t.message.Chat.ID, fileData)
		audio.ReplyToMessageID = t.message.MessageID
		audio.Caption = mediaInfo.title
		audio.Duration = duration
		chattable = audio
	default:
		return fmt.Errorf("unknown media type to do message reply: %s: %w", t.mediaLink.Type, types.ErrInternal)
	}

	logger.Info().Msg("reply media to chat")
	if _, err := t.tg.Send(chattable); err != nil {
		return fmt.Errorf("failed to send media message: %w", err)
	}
	logger.Info().Msg("media sent to chat successfully")

	if err := t.mediaRepo.UpdateState(context.TODO(), t.media.ID, types.DoneMediaState); err != nil {
		logger.Error().Err(err).Msg("failed to update media state to done")
	}

	return nil
}

func getRandomChunkSize() int64 {
	//nolint:gosec // it is ok to have weak generator
	return rand.Int63n(maxChunkSize-minChunkSize) + minChunkSize
}

type job interface {
	do() error
}

func startJob(job job, interval time.Duration, doneC chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		for {
			select {
			case <-ticker.C:
				if err := job.do(); err != nil {
					log.Error().Err(err).Msg("failed to do job")
				}
			case <-doneC:
				ticker.Stop()
				return
			}
		}
	}()
}

// cleanJob is a job which delete media which has 1h and more lifetime.
type cleanJob struct {
	mediaFolder string
}

func (w *cleanJob) do() error {
	if err := filepath.Walk(w.mediaFolder, checkFile); err != nil {
		return fmt.Errorf("failed to walk through media folder: %w", err)
	}
	return nil
}

func checkFile(path string, info fs.FileInfo, err error) error {
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

// prepCbData returns callback data delimiter-ed by topic and value.
func prepCbData(topic, value string) string {
	return fmt.Sprintf("%s%s%s", topic, cbDelimiter, value)
}

func parseCbData(data string) (topic, value string) {
	parts := strings.SplitN(data, cbDelimiter, 2)
	if len(parts) != 2 {
		return
	}
	topic = parts[0]
	value = parts[1]
	return
}

func prepareCMTCbValue(entity types.Entity, mediaType types.MediaType, uri string) string {
	return fmt.Sprintf("%s%s%s%s%s", entity, cbCMTValDelimiter, mediaType, cbCMTValDelimiter, uri)
}

func parseCMTCbValue(value string) (types.Entity, types.MediaType, string) {
	parts := strings.SplitN(value, cbCMTValDelimiter, 3)
	if len(parts) != 3 {
		return "", "", ""
	}
	return types.Entity(parts[0]), types.MediaType(parts[1]), parts[2]
}

func reply(tg *tgbotapi.BotAPI, message *tgbotapi.Message, text string) {
	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ReplyToMessageID = message.MessageID
	if _, err := tg.Send(msg); err != nil {
		log.Error().Err(err).Msg("failed to send message")
	}
}

// reply500 sends internal server error to user.
func reply500(tg *tgbotapi.BotAPI, message *tgbotapi.Message) {
	reply(tg, message, "Something went wrong, try again later")
}

func send(tg *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := tg.Send(msg); err != nil {
		log.Error().Err(err).Msg("failed to send message")
	}
}

func send500(tg *tgbotapi.BotAPI, chatID int64) {
	send(tg, chatID, "Something went wrong, try again later")
}

func removeMarkup(tg *tgbotapi.BotAPI, chatID int64, messageID int) {
	if _, err := tg.Send(&tgbotapi.EditMessageReplyMarkupConfig{
		BaseEdit: tgbotapi.BaseEdit{
			ChatID:    chatID,
			MessageID: messageID,
		},
	}); err != nil {
		log.Error().Err(err).Msg("failed to delete reply buttons from message")
	}
}
