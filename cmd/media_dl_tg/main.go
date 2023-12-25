package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/lavrd/media-dl-tg/internal/config"
	"github.com/lavrd/media-dl-tg/internal/repo"
	"github.com/lavrd/media-dl-tg/internal/types"
	internal_plugin "github.com/lavrd/media-dl-tg/pkg/plugin"
)

const (
	TopicChooseMediaType   = "cmt"
	CallbackDelimiter      = "@"
	CallbackValueDelimiter = ":"
)

const (
	MinChunkSize int64 = 1024 * 100 // 100kb
	MaxChunkSize int64 = 1024 * 200 // 200kb
)

func main() {
	log.Logger = log.
		Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().Caller().Logger().
		Level(zerolog.InfoLevel)
	cfg, err := config.Read()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to read config file")
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

	db, err := repo.OpenDBAndMigrate(cfg.DatabaseFile, cfg.MigrationsPath, repo.ModeRWC)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open database and do migrations")
	}
	usersRepo, mediaRepo := repo.New(db)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err = mediaRepo.DeleteInProgress(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to delete media in progress")
	}

	plugin, err := internal_plugin.Open("./plugin.so")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open plugin")
	}

	doneC := make(chan struct{})
	StartJob(&CleanJob{}, time.Hour, doneC)
	bot, err := NewBot(cfg, plugin, usersRepo, mediaRepo, doneC)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize telegram bot")
	}

	interruptC := make(chan os.Signal, 1)
	defer close(interruptC)
	signal.Notify(interruptC, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	<-interruptC
	log.Debug().Msg("handle SIGINT, SIGQUIT, SIGTERM")

	// Stop receiving updates.
	bot.Stop()
	// Stop all downloads and other routines.
	close(doneC)
	// Close connection with database and other stuff.
	bot.Close()
	if err = db.Close(); err != nil {
		log.Error().Err(err).Msg("failed to close database connection")
	}
	log.Info().Msg("bot has been stopped")
}

type Bot struct {
	tg       *tgbotapi.BotAPI
	updatesC tgbotapi.UpdatesChannel

	plugin internal_plugin.Plugin

	usersRepo repo.UsersRepository
	mediaRepo repo.MediaRepository

	taskC chan Task
	doneC chan struct{}

	chunksWorkers int
}

func NewBot(
	cfg *config.Config, plugin internal_plugin.Plugin,
	usersRepo repo.UsersRepository, mediaRepo repo.MediaRepository,
	doneC chan struct{},
) (*Bot, error) {

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
	taskC := make(chan Task)
	RunWorkers(taskC, cfg.MediaNPlaylistWorkers)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = cfg.TgUpdatesTimeout
	updatesC := tg.GetUpdatesChan(u)
	// Wait for updates and clear them if you don't want to handle a large backlog of old messages.
	time.Sleep(time.Second)
	updatesC.Clear()

	bot := &Bot{
		tg:       tg,
		updatesC: updatesC,

		plugin: plugin,

		usersRepo: usersRepo,
		mediaRepo: mediaRepo,

		taskC: taskC,
		doneC: doneC,

		chunksWorkers: cfg.ChunksWorkers,
	}
	go bot.HandleUpdates()

	return bot, nil
}

func (b *Bot) Stop() {
	b.tg.StopReceivingUpdates()
	b.updatesC.Clear()
}

func (b *Bot) Close() {
	close(b.taskC)
}

func (b *Bot) HandleUpdates() {
	log.Info().Msg("bot has started and is waiting for updates")
	for {
		select {
		case update := <-b.updatesC:
			if from, err := b.HandleUpdate(update); err != nil {
				log.Error().Err(err).Msg("failed to handle update")
				if from != 0 {
					b.Send(from, nil, nil, "Something went wrong. Try again.", nil, false)
				}
			}
		case <-b.doneC:
			return
		}
	}
}

func (b *Bot) HandleUpdate(update tgbotapi.Update) (int64, error) {
	var from int64
	var text string
	switch {
	case update.Message != nil:
		from = update.Message.From.ID
		text = update.Message.Text
	case update.CallbackQuery != nil && update.CallbackQuery.Message != nil:
		from = update.CallbackQuery.Message.Chat.ID
		text = update.CallbackQuery.Message.Text
	default:
		return 0, fmt.Errorf("update is not message and callback: %w", types.ErrInternal)
	}
	if err := b.ProcessUpdate(update, from, text); err != nil {
		return from, fmt.Errorf("failed to process update: %w", err)
	}
	return 0, nil
}

func (b *Bot) ProcessUpdate(update tgbotapi.Update, from int64, text string) error {
	user, err := b.CheckUser(from)
	if err != nil {
		return fmt.Errorf("failed to check user: %w", err)
	}
	// It means we don't need to continue flow.
	// For example, it was start command, and we just created user.
	if user == nil {
		return nil
	}
	if update.Message != nil && update.Message.IsCommand() {
		return b.HandleCommand(update.Message, user)
	}
	switch {
	case update.Message != nil:
		err = b.HandleMessage(update.Message, user)
	case update.CallbackQuery != nil:
		err = b.HandleCallback(update.CallbackQuery, user, b.doneC)
	default:
		return fmt.Errorf("unkwnon case: %w", types.ErrInternal)
	}
	if err != nil {
		return fmt.Errorf("failed to handle new update: %w", err)
	}
	return nil
}

// todo: resolve all context.TODO
// todo: use logger context instead of just instance?

func (b *Bot) HandleMessage(message *tgbotapi.Message, user *types.User) error {
	if message.IsCommand() {
		return b.HandleCommand(message, user)
	}

	entityType, entityID, err := b.plugin.ParseEntity(message.Text)
	if err != nil {
		b.Reply(message.From.ID, &message.MessageID, "Link to the media or playlist is incorrect.")
		return nil
	}
	switch entityType {
	case internal_plugin.EntityMedia:
	case internal_plugin.EntityPlaylist:
		if user.PlaylistMaxSize == 0 {
			b.Reply(message.From.ID, &message.MessageID, "You are not allowed to download playlists.")
			return nil
		}
	default:
		return fmt.Errorf("unknown entity type: %s: %w", entityType, err)
	}

	text := "Choose type: audio or video. " +
		"Most likely the audio will have better sound quality and a much smaller file size."
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📹 Video",
				prepareCallbackData(
					TopicChooseMediaType,
					prepareCallbackValue(entityType, internal_plugin.VideoMediaType, entityID))),
			tgbotapi.NewInlineKeyboardButtonData("🎧 Audio",
				prepareCallbackData(
					TopicChooseMediaType,
					prepareCallbackValue(entityType, internal_plugin.AudioMediaType, entityID))),
		),
	)
	b.Send(message.From.ID, nil, &keyboard, text, &message.MessageID, true)
	return nil
}

func (b *Bot) HandleCommand(message *tgbotapi.Message, user *types.User) error {
	switch message.Command() {
	case "me":
		b.Reply(message.From.ID, &message.MessageID, strconv.Itoa(int(message.From.ID)))
		return nil
	case "pending":
		media, err := b.mediaRepo.GetInProgress(context.TODO(), user.ID)
		if err != nil {
			return fmt.Errorf("failed to get media in progress: %w", err)
		}
		if len(media) == 0 {
			b.Reply(message.From.ID, &message.MessageID, "You don't have media in progress.")
			return nil
		}
		text := "Media in progress"
		for _, entity := range media {
			name := entity.Title
			if name == "" {
				name = entity.URI
			}
			text = fmt.Sprintf("%s\n%s", text, name)
		}
		b.Reply(message.From.ID, &message.MessageID, text)
		return nil
	case "help":
		b.Reply(message.From.ID, &message.MessageID, "Sorry, I am too lazy to write it.")
		return nil
	default:
		return nil
	}
}

// CheckUser returns user as nil if we don't need to continue flow; for example if user is banned.
func (b *Bot) CheckUser(tgUserID int64) (*types.User, error) {
	user, err := b.usersRepo.Get(context.TODO(), tgUserID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = b.usersRepo.Create(context.TODO(), tgUserID); err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		b.Reply(tgUserID, nil, "Send link to start downloading.")
		return nil, nil
	}
	if user.AudioMaxSize == 0 && user.VideoMaxSize == 0 {
		b.Reply(tgUserID, nil, "You are not allowed to download media.")
		return nil, nil
	}
	return user, nil
}

func (b *Bot) HandleCallback(callback *tgbotapi.CallbackQuery, user *types.User, doneC chan struct{}) error {
	topic, value := parseCbData(callback.Data)
	switch topic {
	case TopicChooseMediaType:
		if err := b.HandleChooseMediaType(callback.Message, value, user, doneC); err != nil {
			return fmt.Errorf("failed to choose media type: %w", err)
		}
		b.RemoveMarkup(callback.Message.Chat.ID, callback.Message.MessageID)
		return nil
	default:
		return fmt.Errorf("unknown topic: %s: %w", topic, types.ErrInternal)
	}
}

func (b *Bot) HandleChooseMediaType(
	message *tgbotapi.Message, value string, user *types.User, doneC chan struct{},
) error {

	entityType, mediaTyp, uri := parseCallbackValue(value)
	mediaLink := &types.MediaLink{URI: uri, Type: mediaTyp}

	/*
		Following code protects from multiple simultaneously downloads by one user.
	*/
	multimedia, err := b.mediaRepo.GetInProgress(context.TODO(), user.ID)
	if err != nil {
		return fmt.Errorf("failed to get media in progress: %w", err)
	}
	if len(multimedia) != 0 {
		b.Reply(message.From.ID, &message.MessageID, "You already have media which are in progress.")
		return nil
	}

	var downloadTask Task
	switch entityType {
	case internal_plugin.EntityPlaylist:
		downloadTask = newDownloadPlaylistTask(b.plugin,
			b.tg, message, user, media.ID, mediaLink, b.mediaRepo, b.taskC, b.chunksWorkers, doneC)
	case internal_plugin.EntityMedia:
		downloadTask = NewDownloadMediaTask(b.plugin, mediaLink, "", b.chunksWorkers, doneC)
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

func (b *Bot) Reply(chatID int64, messageID *int, text string) {
	b.Send(chatID, nil, nil, text, messageID, true)
}

func (b *Bot) RemoveMarkup(chatID int64, messageID int) {
	if _, err := b.tg.Send(&tgbotapi.EditMessageReplyMarkupConfig{
		BaseEdit: tgbotapi.BaseEdit{
			ChatID:    chatID,
			MessageID: messageID,
		},
	}); err != nil {
		log.Error().Err(err).Msg("failed to delete reply buttons from message")
	}
}

func (b *Bot) Send(
	chatID int64,
	replyKeyboard *tgbotapi.ReplyKeyboardMarkup,
	inlineKeyboard *tgbotapi.InlineKeyboardMarkup,
	text string, replyTo *int, notify bool,
) {
	msg := tgbotapi.NewMessage(chatID, text)
	if replyTo != nil {
		msg.ReplyToMessageID = *replyTo
	}
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(false)
	msg.DisableNotification = !notify
	if replyKeyboard != nil {
		msg.ReplyMarkup = replyKeyboard
	}
	if inlineKeyboard != nil {
		msg.ReplyMarkup = inlineKeyboard
	}
	if _, err := b.tg.Send(msg); err != nil {
		log.Error().Err(err).Msg("failed to send message to user")
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

func getRandomChunkSize() int64 {
	//nolint:gosec // it is ok to have weak generator
	return rand.Int63n(MaxChunkSize-MinChunkSize) + MinChunkSize
}

// prepCbData returns callback data delimiter-ed by topic and value.
func prepareCallbackData(topic, value string) string {
	return fmt.Sprintf("%s%s%s", topic, CallbackDelimiter, value)
}

func parseCbData(data string) (topic, value string) {
	parts := strings.SplitN(data, CallbackDelimiter, 2)
	if len(parts) != 2 {
		return
	}
	topic = parts[0]
	value = parts[1]
	return
}

func prepareCallbackValue(entity internal_plugin.Entity, mediaType internal_plugin.MediaType, uri string) string {
	return fmt.Sprintf("%s%s%s%s%s", entity, CallbackValueDelimiter, mediaType, CallbackValueDelimiter, uri)
}

func parseCallbackValue(value string) (internal_plugin.Entity, internal_plugin.MediaType, string) {
	parts := strings.SplitN(value, CallbackValueDelimiter, 3)
	if len(parts) != 3 {
		return "", "", ""
	}
	return internal_plugin.Entity(parts[0]), internal_plugin.MediaType(parts[1]), parts[2]
}
