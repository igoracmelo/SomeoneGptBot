package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

var groupID int64
var basename string
var botUsername string
var token string
var mediaProb float64
var markovProb float64

func mustNotBeZero[T comparable](name string, t T) {
	var t2 T
	if t == t2 {
		panic(fmt.Sprintf("'%s' is zero ('%+v')", name, t))
	}
}

func main() {
	flag.Int64Var(&groupID, "group", 0, "")
	flag.StringVar(&token, "token", "", "")
	flag.StringVar(&basename, "base", "", "")
	flag.StringVar(&botUsername, "botuser", "", "")
	flag.Float64Var(&mediaProb, "medp", 0, "")
	flag.Float64Var(&markovProb, "markp", 0, "")
	flag.Parse()

	mustNotBeZero("base", basename)
	mustNotBeZero("botuser", botUsername)
	mustNotBeZero("token", token)

	c := &ctx{}

	err := c.init()
	if err != nil {
		panic(err)
	}

	defer c.stop()

	go c.handleStdin()


	// c.handler.Handle(func(bot *telego.Bot, u telego.Update) {
	// 	err = c.sendRandomMedia(u.Message.Chat.ID, u.Message.MessageID)
	// 	if err != nil {
	// 		log.Print(err)
	// 	}
	// }, th.CommandEqual("media"))

	c.handler.Handle(func(bot *telego.Bot, u telego.Update) {
		seq := c.chain.makeSequence()
		txt := ""

		for _, token := range seq {
			if len(token) == 1 && unicode.IsPunct([]rune(token)[0]) {
				txt += token
			} else {
				txt += " " + token
			}
		}

		_, err := c.bot.SendMessage(&telego.SendMessageParams{
			ChatID: telego.ChatID{
				ID: u.Message.Chat.ID,
			},
			Text:             txt,
			ReplyToMessageID: u.Message.MessageID,
		})
		if err != nil {
			log.Print(err)
		}
	}, th.CommandEqual("markov"))

	c.handler.Handle(func(bot *telego.Bot, u telego.Update) {
		// if u
		// log.Print(u.Message.MessageID, " ", u.Message.Text)
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
	chatMu  map[int64]*sync.Mutex
	chain   chain
}

type media struct {
	fileID string
	kind   string
}

func (c *ctx) lockChat(chatID int64) {
	mu := c.chatMu[chatID]
	if mu == nil {
		mu = new(sync.Mutex)
		c.chatMu[chatID] = mu
	}
	log.Printf("locking chat %v", chatID)
	mu.Lock()
}

func (c *ctx) unlockChat(chatID int64) {
	mu := c.chatMu[chatID]
	if mu == nil {
		panic("tried to unlock chat that wasn't locked")
	}
	log.Printf("unlocking chat %v", chatID)
	mu.Unlock()
}

func (c *ctx) init() error {
	c.chatMu = map[int64]*sync.Mutex{}

	bot, err := telego.NewBot(token)
	if err != nil {
		return err
	}
	c.bot = bot

	go func() {
		_, err = bot.GetMe()
		if err != nil {
			panic(err)
		}
	}()

	updates, err := bot.UpdatesViaLongPolling(nil)
	if err != nil {
		return err
	}

	h, err := th.NewBotHandler(bot, updates)
	if err != nil {
		return err
	}
	c.handler = h

	f, err := os.Open("messages/" + basename + ".txt")
	if err != nil {
		return err
	}

	t1 := time.Now()
	c.chain = buildMarkovChain(f)
	f.Close()
	took := time.Now().Sub(t1)
	log.Printf("took %d ms to build markov chain", took.Milliseconds())

	b, err := os.ReadFile("messages/" + basename + ".txt")
	if err != nil {
		return err
	}
	c.lines = bytes.Split(b, []byte("\n"))

	f, err = os.Open("medias/" + basename + ".csv")
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
			ReplyToMessageID: msgID,
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
			ReplyToMessageID: msgID,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *ctx) sendMarkovSequence(chatID int64, msgID int) error {
	seq := c.chain.makeSequence()
	txt := ""
	for _, token := range seq {
		r := []rune(token)[0]
		if len(token) == 1 && !unicode.IsDigit(r) && !unicode.IsLetter(r) {
			txt += token
		} else {
			txt += " " + token
		}
	}

	log.Printf("markovei: %s", txt)

	_, err := c.bot.SendMessage(&telego.SendMessageParams{
		ChatID: telego.ChatID{
			ID: chatID,
		},
		Text:             txt,
		ReplyToMessageID: msgID,
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *ctx) handleRandom(chatID int64, msgID int) {
	c.lockChat(chatID)
	defer c.unlockChat(chatID)

	count := rand.Intn(2) + 1
	log.Printf("handleRandom() -- %v replies", count)
	time.Sleep(time.Second * time.Duration(rand.Intn(2)+1))
	type sendFn = func(int64, int) error

	mediaProb := mediaProb / float64(count)
	sentMedia := false

	replyIndex := rand.Intn(count)
	for i := 0; i < count; i++ {
		var err error
		var send sendFn = c.sendRandomText
		action := "typing"
		minSleep := 3
		if !sentMedia && rand.Float64() < mediaProb && len(c.medias) > 0 {
			send = c.sendRandomMedia
			action = "choose_sticker"
			sentMedia = true
			minSleep = 5
		} else if rand.Float64() < markovProb {
			send = c.sendMarkovSequence
		}

		_ = c.bot.SendChatAction(&telego.SendChatActionParams{
			ChatID: telego.ChatID{
				ID: chatID,
			},
			Action: action,
		})


		time.Sleep(time.Second * time.Duration(rand.Intn(3)+minSleep))

		if i == replyIndex {
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

	log.Printf("do arquivo: %s", line)

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

type chain struct {
	firsts []string
	data   map[string][]string
}

func buildMarkovChain(r io.Reader) chain {
	scanner := bufio.NewScanner(r)
	re := regexp.MustCompile(`(\p{L}+)|(\S+)`)
	firstsCh := make(chan string, runtime.NumCPU())
	tokensCh := make(chan [2]string, runtime.NumCPU())

	go func() {
		limit := make(chan struct{}, runtime.NumCPU())
		wg := new(sync.WaitGroup)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			limit <- struct{}{}
			wg.Add(1)

			go func() {
				defer wg.Done()
				defer func() { <-limit }()
				tokens := re.FindAllString(line, -1)
				if len(tokens) == 0 {
					return
				}
				firstsCh <- tokens[0]
				for i := 0; i < len(tokens)-1; i++ {
					curr := tokens[i]
					next := tokens[i+1]
					tokensCh <- [2]string{curr, next}
				}
			}()
		}

		wg.Wait()
		close(firstsCh)
		close(tokensCh)
	}()

	chain := chain{
		firsts: make([]string, 0, 28000),
		data:   make(map[string][]string, 28000),
	}

	done := make(chan struct{})
	go func() {
		for first := range firstsCh {
			chain.firsts = append(chain.firsts, first)
		}
		done <- struct{}{}
	}()

	go func() {
		for pair := range tokensCh {
			curr := pair[0]
			next := pair[1]
			chain.data[curr] = append(chain.data[curr], next)
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	return chain
}

func (c *chain) makeSequence() []string {
	res := []string{}

	count := 0
	for range c.data {
		count++
	}

	first := c.firsts[rand.Intn(len(c.firsts))]
	res = append(res, first)
	curr := first
	for {
		if len(res) > 10 {
			return c.makeSequence()
		}
		choices := c.data[curr]
		if len(choices) <= 2 {
			break
		}
		curr = choices[rand.Intn(len(choices))]
		res = append(res, curr)
	}

	if len(res) < 4 {
		return c.makeSequence()
	}

	return res
}
