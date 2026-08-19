package read

import (
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/arikawa/v3/state"
	"github.com/ayn2op/arikawa/v3/state/store/defaultstore"
	"github.com/ayn2op/arikawa/v3/utils/handler"
)

func TestChannelUnreadUpdateSetsLastMessage(t *testing.T) {
	const (
		guildID   = discord.GuildID(1)
		channelID = discord.ChannelID(2)
		messageID = discord.MessageID(3)
	)

	cabinet := defaultstore.New()
	if err := cabinet.ChannelSet(&discord.Channel{ID: channelID, GuildID: guildID}, false); err != nil {
		t.Fatal(err)
	}

	h := handler.New()
	NewState(&state.State{Cabinet: cabinet}, h)

	event := &gateway.ChannelUnreadUpdateEvent{GuildID: guildID}
	event.ChannelUnreadUpdates = append(event.ChannelUnreadUpdates, struct {
		ID            discord.ChannelID `json:"id"`
		LastMessageID discord.MessageID `json:"last_message_id"`
	}{ID: channelID, LastMessageID: messageID})
	h.Call(event)

	channel, err := cabinet.Channel(channelID)
	if err != nil {
		t.Fatal(err)
	}
	if channel.LastMessageID != messageID {
		t.Fatalf("LastMessageID = %d, want %d", channel.LastMessageID, messageID)
	}
}
