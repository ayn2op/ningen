package ningen

import (
	"fmt"
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/state"
	"github.com/ayn2op/arikawa/v3/state/store/defaultstore"
)

var lastMessageSink discord.MessageID

func TestLastMessagePrefersChannel(t *testing.T) {
	const (
		guildID          = discord.GuildID(1)
		channelID        = discord.ChannelID(2)
		cachedMessageID  = discord.MessageID(3)
		channelMessageID = discord.MessageID(4)
	)

	cabinet := defaultstore.New()
	if err := cabinet.ChannelSet(&discord.Channel{ID: channelID, GuildID: guildID, LastMessageID: channelMessageID}, false); err != nil {
		t.Fatal(err)
	}
	if err := cabinet.MessageSet(&discord.Message{ID: cachedMessageID, ChannelID: channelID}, false); err != nil {
		t.Fatal(err)
	}

	s := &State{State: &state.State{Cabinet: cabinet}}
	if got := s.LastMessage(channelID); got != channelMessageID {
		t.Fatalf("LastMessage() = %d, want channel last message %d", got, channelMessageID)
	}
}

func TestLastMessageFallsBackToCache(t *testing.T) {
	const (
		channelID = discord.ChannelID(1)
		messageID = discord.MessageID(2)
	)

	cabinet := defaultstore.New()
	if err := cabinet.MessageSet(&discord.Message{ID: messageID, ChannelID: channelID}, false); err != nil {
		t.Fatal(err)
	}

	s := &State{State: &state.State{Cabinet: cabinet}}
	if got := s.LastMessage(channelID); got != messageID {
		t.Fatalf("LastMessage() = %d, want cached message %d", got, messageID)
	}
}

func BenchmarkLastMessage(b *testing.B) {
	for _, messages := range []int{1, 50, 100} {
		b.Run(fmt.Sprintf("messages=%d", messages), func(b *testing.B) {
			benchmarkLastMessage(b, messages)
		})
	}
}

func benchmarkLastMessage(b *testing.B, messages int) {
	const (
		guildID   = discord.GuildID(1)
		channelID = discord.ChannelID(2)
	)

	cabinet := defaultstore.New()
	for id := discord.MessageID(1); id <= discord.MessageID(messages); id++ {
		if err := cabinet.MessageSet(&discord.Message{ID: id, ChannelID: channelID}, false); err != nil {
			b.Fatal(err)
		}
	}
	if err := cabinet.ChannelSet(&discord.Channel{
		ID: channelID, GuildID: guildID, LastMessageID: discord.MessageID(messages + 1),
	}, false); err != nil {
		b.Fatal(err)
	}

	s := &State{State: &state.State{Cabinet: cabinet}}

	b.Run("channel_cursor", func(b *testing.B) {
		for b.Loop() {
			lastMessageSink = s.LastMessage(channelID)
		}
	})

	b.Run("message_slice_clone", func(b *testing.B) {
		for b.Loop() {
			lastMessageSink = lastMessageFromCache(s, channelID)
		}
	})
}

func lastMessageFromCache(s *State, channelID discord.ChannelID) discord.MessageID {
	msgs, _ := s.Cabinet.Messages(channelID)
	if len(msgs) > 0 {
		return msgs[0].ID
	}

	ch, _ := s.Cabinet.Channel(channelID)
	if ch != nil {
		return ch.LastMessageID
	}

	return 0
}
