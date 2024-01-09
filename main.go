package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/sashabaranov/go-openai"
)

var (
	BotToken          = os.Getenv("DISCORD_BOT_TOKEN")
	BotName           = os.Getenv("BOT_NAME")
	AppID             = os.Getenv("APP_ID")
	GuildID           = os.Getenv("GUILD_ID")
	AdminRoleID       = os.Getenv("ADMIN_ROLE_ID")
	ChatAllowedRoleID = os.Getenv("CHAT_ALLOWED_ROLE_ID")
	ChannelID         = os.Getenv("CHANNEL_ID")
	PromptChannelID   = os.Getenv("PROMPT_CHANNEL_ID")
	OpenAIToken       = os.Getenv("OPENAI_TOKEN")
)

var (
	Prompts string
	l       sync.RWMutex
)

func fetchPrompts(discord *discordgo.Session) error {
	l.Lock()
	defer l.Unlock()
	mm, err := discord.ChannelMessagesPinned(PromptChannelID)
	if err != nil {
		return fmt.Errorf("error retrieving pinned posts: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("Your name is " + BotName + ".\n")
	for _, m := range mm {
		sb.WriteString(m.Content + "\n")
	}
	Prompts = sb.String()
	return nil
}

func main() {
	l, err := net.Listen("tcp4", "0.0.0.0:8080")
	if err != nil {
		panic(err)
	}
	defer l.Close()

	aiClient := openai.NewClient(OpenAIToken)

	discord, err := discordgo.New("Bot " + BotToken)
	if err != nil {
		panic(err)
	}
	if err := discord.Open(); err != nil {
		panic(err)
	}
	defer discord.Close()

	// do initial load of prompts
	fetchPrompts(discord)

	cmd, err := discord.ApplicationCommandCreate(AppID, GuildID, &discordgo.ApplicationCommand{
		Name:        "refresh-prompt",
		Description: "Admin command to refresh prompt",
	})
	if err != nil {
		panic(err)
	}
	defer discord.ApplicationCommandDelete(AppID, GuildID, cmd.ID)

	defer discord.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			if i.ApplicationCommandData().Name == "refresh-prompt" {
				if !canChat(i.Member) {
					return
				}

				fetchPrompts(s)
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "Refreshed prompt.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
			}
		}
	})()

	buf := NewChatBuffer()

	defer discord.AddHandler(func(s *discordgo.Session, i *discordgo.MessageCreate) {
		if i.ChannelID != ChannelID {
			return
		}
		if !canChat(i.Member) {
			return
		}
		if !strings.Contains(strings.ToLower(i.Content), BotName) {
			return
		}
		s.ChannelTyping(ChannelID)

		resp, err := callChatGPT(aiClient, buf, i.Content)
		if err != nil {
			log.Println(err)
			return
		}
		if _, err := s.ChannelMessageSendReply(ChannelID, resp, i.Reference()); err != nil {
			log.Println("error sending reply: " + err.Error())
		}
	})()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press ctrl+c to exit")
	<-stop

	log.Println("exiting")
}

func canChat(m *discordgo.Member) bool {
	if m == nil {
		return false
	}
	for _, r := range m.Roles {
		if r == AdminRoleID || r == ChatAllowedRoleID {
			return true
		}
	}
	return false
}

func callChatGPT(aiClient *openai.Client, buf *ChatBuffer, msg string) (string, error) {
	buf.Add(openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: msg,
	})

	resp, err := aiClient.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
		Model:    openai.GPT3Dot5Turbo,
		Messages: buf.Msgs(),
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) < 1 {
		return "", fmt.Errorf("expected one Choice, got none: %v", resp)
	}
	buf.Add(openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: resp.Choices[0].Message.Content,
	})
	return resp.Choices[0].Message.Content, nil
}
