package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/igoracmelo/SomeoneGptBot/env"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

var groupID = env.MustInt64("GROUP_ID")
var basename = env.Must("BASENAME")
var botUsername = env.Must("BOT_USERNAME")
var realUsername = env.Must("REAL_USERNAME")
var token = env.Must("TOKEN")

func main() {
	c := &ctx{}

	err := c.init()
	if err != nil {
		panic(err)
	}
	defer c.stop()

	go c.handleStdin()

	c.handler.Handle(func(bot *telego.Bot, u telego.Update) {
		err = c.sendRandomMedia(u.Message.Chat.ID, u.Message.MessageID)
		if err != nil {
			log.Print(err)
		}
	}, th.CommandEqual("media"))

	c.handler.Handle(func(bot *telego.Bot, u telego.Update) {
		replyMsgID := u.Message.MessageID

		mentionMe := strings.Contains(u.Message.Text, "@"+botUsername)
		replyToMe := u.Message.ReplyToMessage != nil && u.Message.ReplyToMessage.From.Username == botUsername
		replyToOther := u.Message.ReplyToMessage != nil && u.Message.ReplyToMessage.From.Username != botUsername

		if u.Message.Chat.Type == "private" {
			c.handleRandom(u.Message.Chat.ID, replyMsgID)
			return
		}

		if replyToMe {
			c.handleRandom(u.Message.Chat.ID, replyMsgID)
			return
		}

		if mentionMe {
			if replyToOther {
				replyMsgID = u.Message.ReplyToMessage.MessageID
			}
			c.handleRandom(u.Message.Chat.ID, replyMsgID)
			return
		}
	}, th.AnyMessage())

	c.handler.Start()
}

type ctx struct {
	bot     *telego.Bot
	handler *th.BotHandler
	lines   [][]byte
	medias  []media
}

type media struct {
	fileID string
	kind   string
}

func (c *ctx) init() error {
	bot, err := telego.NewBot(token)
	if err != nil {
		return err
	}
	c.bot = bot

	_, err = bot.GetMe()
	if err != nil {
		return err
	}

	updates, err := bot.UpdatesViaLongPolling(nil)
	if err != nil {
		return err
	}

	h, err := th.NewBotHandler(bot, updates)
	if err != nil {
		return err
	}
	c.handler = h

	b, err := os.ReadFile("messages/" + basename + ".txt")
	if err != nil {
		return err
	}
	c.lines = bytes.Split(b, []byte("\n"))

	f, err := os.Open("medias/" + basename + ".csv")
	if errors.Is(err, os.ErrNotExist) {
		c.medias = []media{}
		return nil
	}

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return err
	}

	c.medias = make([]media, len(records))

	for i, record := range records {
		m := media{}
		m.fileID = record[2]
		m.kind = record[3]

		c.medias[i] = m
	}

	return nil
}

func (c *ctx) stop() error {
	c.bot.StopLongPolling()
	c.handler.Stop()
	return nil
}

func (c *ctx) sendRandomMedia(chatID int64, msgID int) error {
	if len(c.medias) == 0 {
		_, err := c.bot.SendMessage(&telego.SendMessageParams{
			ChatID: telego.ChatID{
				ID: chatID,
			},
			Text: "esse usuário não tem gifs / stickers registrados",
		})
		if err != nil {
			return err
		}
	}

	i := rand.Intn(len(c.medias))
	m := c.medias[i]

	if m.kind == "animation" {
		_, err := c.bot.SendAnimation(&telego.SendAnimationParams{
			ChatID: telego.ChatID{
				ID: chatID,
			},
			Animation: telego.InputFile{
				FileID: m.fileID,
			},
		})
		if err != nil {
			return err
		}
	} else if m.kind == "sticker" {
		_, err := c.bot.SendSticker(&telego.SendStickerParams{
			ChatID: telego.ChatID{
				ID: chatID,
			},
			Sticker: telego.InputFile{
				FileID: m.fileID,
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *ctx) handleRandom(chatID int64, msgID int) {
	count := rand.Intn(2) + 1
	log.Printf("handleRandom() -- %v replies", count)

	type sendFn = func(int64, int) error

	prob := 0.5 / float64(count)

	for i := 0; i < count; i++ {
		var err error
		var send sendFn = c.sendRandomText
		action := "typing"
		minSleep := 3
		if rand.Float64() < prob && len(c.medias) > 0 {
			send = c.sendRandomMedia
			action = "choose_sticker"
			minSleep = 5
		}

		_ = c.bot.SendChatAction(&telego.SendChatActionParams{
			ChatID: telego.ChatID{
				ID: chatID,
			},
			Action: action,
		})

		time.Sleep(time.Second * time.Duration(rand.Intn(3)+minSleep))

		if i == 0 {
			err = send(chatID, msgID)
		} else {
			err = send(chatID, 0)
		}

		if err != nil {
			log.Print(err)
			return
		}
	}
}

func (c *ctx) sendRandomText(chatID int64, msgID int) error {
	line := ""
	for len(strings.TrimSpace(line)) == 0 {
		i := rand.Intn(len(c.lines))
		line = strings.TrimSpace(string(c.lines[i]))
	}

	_, err := c.bot.SendMessage(&telego.SendMessageParams{
		ChatID: telego.ChatID{
			ID: chatID,
		},
		Text:             line,
		ReplyToMessageID: msgID,
	})
	if err != nil {
		return err
	}

	return nil
}
func (c *ctx) handleStdin() {
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		if s.Text() == "" {
			continue
		}
		c.handleRandom(groupID, 0)
	}
}

func writeMedia(msg *telego.Message) error {
	fromID := fmt.Sprint(msg.From.ID)
	var kind string
	var fileID string

	if msg.Sticker != nil {
		kind = "sticker"
		fileID = msg.Sticker.FileID
	} else {
		kind = "animation"
		fileID = msg.Animation.FileID
	}

	row := []string{
		fmt.Sprint(fromID),
		fmt.Sprint(msg.Chat.ID),
		fileID,
		kind,
	}

	name := senderName(msg.From)
	if msg.ForwardFrom != nil {
		name = senderName(msg.ForwardFrom)
	}
	filename := "./medias/" + name + ".csv"

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	err = w.Write(row)
	if err != nil {
		return err
	}
	w.Flush()

	err = w.Error()
	if err != nil {
		return err
	}

	return nil
}

func senderName(user *telego.User) string {
	if user.Username != "" {
		return strings.ToLower(user.Username)
	}
	return strings.ToLower(sanitize(user.FirstName))
}

func sanitize(s string) string {
	res := ""
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			res += string(r)
		}
	}
	return strings.TrimSpace(res)
}
