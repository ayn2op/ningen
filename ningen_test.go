package ningen

import (
	"fmt"
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/arikawa/v3/state"
	"github.com/ayn2op/arikawa/v3/state/store"
	"github.com/ayn2op/arikawa/v3/state/store/defaultstore"
)

var lastMessageSink discord.MessageID
var unreadSink UnreadIndication

type countingRoleStore struct {
	store.RoleStore
	reads int
}

func (s *countingRoleStore) Roles(guildID discord.GuildID) ([]discord.Role, error) {
	s.reads++
	return s.RoleStore.Roles(guildID)
}

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

func TestChannelIsUnreadSkipsPermissionsWhenAlreadyRead(t *testing.T) {
	s, roles := newUnreadTestState(t, 100, 100)

	if got := s.ChannelIsUnread(2, UnreadOpts{}); got != ChannelRead {
		t.Fatalf("ChannelIsUnread() = %v, want ChannelRead", got)
	}
	if roles.reads != 0 {
		t.Fatalf("permission role reads = %d, want 0", roles.reads)
	}
}

func TestChannelIsUnreadChecksPermissionsWhenUnread(t *testing.T) {
	s, roles := newUnreadTestState(t, 99, 100)

	if got := s.ChannelIsUnread(2, UnreadOpts{}); got != ChannelUnread {
		t.Fatalf("ChannelIsUnread() = %v, want ChannelUnread", got)
	}
	if roles.reads == 0 {
		t.Fatal("permission roles were not read for an unread channel")
	}
}

func BenchmarkChannelIsUnreadAlreadyRead(b *testing.B) {
	s, _ := newUnreadTestState(b, 100, 100)

	b.Run("cursor_fast_path", func(b *testing.B) {
		for b.Loop() {
			unreadSink = s.ChannelIsUnread(2, UnreadOpts{})
		}
	})
	b.Run("permission_first", func(b *testing.B) {
		for b.Loop() {
			unreadSink = channelIsUnreadPermissionFirst(s, 2, UnreadOpts{})
		}
	})
}

func newUnreadTestState(tb testing.TB, acked, latest discord.MessageID) (*State, *countingRoleStore) {
	tb.Helper()

	const (
		guildID   = discord.GuildID(1)
		channelID = discord.ChannelID(2)
		userID    = discord.UserID(3)
	)

	s := FromState(state.NewWithStore("", defaultstore.New()))
	s.State.Handler.Call(&gateway.ReadyEvent{
		User: discord.User{ID: userID},
		ReadyEventExtras: gateway.ReadyEventExtras{ReadStates: []gateway.ReadState{{
			ChannelID: channelID, LastMessageID: acked,
		}}},
	})

	if err := s.Cabinet.MyselfSet(discord.User{ID: userID}, false); err != nil {
		tb.Fatal(err)
	}
	if err := s.Cabinet.GuildSet(&discord.Guild{ID: guildID}, false); err != nil {
		tb.Fatal(err)
	}
	if err := s.Cabinet.ChannelSet(&discord.Channel{
		ID: channelID, GuildID: guildID, LastMessageID: latest,
	}, false); err != nil {
		tb.Fatal(err)
	}
	if err := s.Cabinet.MemberSet(guildID, &discord.Member{User: discord.User{ID: userID}}, false); err != nil {
		tb.Fatal(err)
	}
	if err := s.Cabinet.RoleSet(guildID, &discord.Role{
		ID: discord.RoleID(guildID), Permissions: discord.PermissionViewChannel,
	}, false); err != nil {
		tb.Fatal(err)
	}

	roles := &countingRoleStore{RoleStore: s.Cabinet.RoleStore}
	s.Cabinet.RoleStore = roles
	return s, roles
}

func channelIsUnreadPermissionFirst(s *State, channelID discord.ChannelID, opts UnreadOpts) UnreadIndication {
	readState := s.ReadState.ReadState(channelID)
	if readState == nil || !readState.LastMessageID.IsValid() {
		return ChannelRead
	}
	if readState.MentionCount > 0 {
		return ChannelMentioned
	}
	if s.ChannelIsMuted(channelID, opts) {
		return ChannelRead
	}
	latest := s.LastMessage(channelID)
	if !latest.IsValid() {
		return ChannelRead
	}
	if !s.HasPermissions(channelID, discord.PermissionViewChannel) {
		return ChannelRead
	}
	if readState.LastMessageID < latest {
		return ChannelUnread
	}
	return ChannelRead
}
