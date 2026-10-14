package read

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ayn2op/arikawa/v3/api"
	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/arikawa/v3/session"
	"github.com/ayn2op/arikawa/v3/state"
	"github.com/ayn2op/arikawa/v3/state/store/defaultstore"
	"github.com/ayn2op/arikawa/v3/utils/handler"
	"github.com/ayn2op/arikawa/v3/utils/httputil"
	"github.com/ayn2op/arikawa/v3/utils/httputil/httpdriver"
)

type ackDriver struct {
	requests chan *httpdriver.MockRequest
}

func (d *ackDriver) NewRequest(ctx context.Context, method, url string) (httpdriver.Request, error) {
	return httpdriver.NewMockRequestWithContext(ctx, method, url, nil, nil), nil
}

func (d *ackDriver) Do(request httpdriver.Request) (httpdriver.Response, error) {
	d.requests <- request.(*httpdriver.MockRequest)
	return httpdriver.NewMockResponse(http.StatusOK, http.Header{}, struct{}{}), nil
}

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

func TestMarkReadAcksUncachedChannelCursor(t *testing.T) {
	const (
		channelID = discord.ChannelID(2)
		messageID = discord.MessageID(3)
	)

	driver := &ackDriver{requests: make(chan *httpdriver.MockRequest, 1)}
	client := api.NewCustomClient("token", httputil.NewClientWithDriver(driver))
	session := session.NewCustom(gateway.DefaultIdentifier("token"), client, handler.New())
	cabinet := defaultstore.New()
	if err := cabinet.ChannelSet(&discord.Channel{ID: channelID, GuildID: 1}, false); err != nil {
		t.Fatal(err)
	}

	stateHandler := handler.New()
	readHandler := handler.New()
	readState := NewState(&state.State{
		Session: session, Cabinet: cabinet, Handler: stateHandler,
	}, readHandler)
	readHandler.Call(&gateway.ReadyEvent{User: discord.User{ID: 4}})

	readState.MarkRead(channelID, messageID)

	select {
	case request := <-driver.requests:
		want := "/api/v9/channels/2/messages/3/ack"
		if request.GetPath() != want {
			t.Fatalf("ack path = %q, want %q", request.GetPath(), want)
		}
	case <-time.After(time.Second):
		t.Fatal("MarkRead did not acknowledge an uncached channel cursor")
	}
}

func TestStateReadState(t *testing.T) {
	// read returns the state of user 4, who has read message 3 of forum 2, after events.
	read := func(t *testing.T, events ...any) *State {
		t.Helper()
		cabinet := defaultstore.New()
		if err := cabinet.ChannelSet(&discord.Channel{ID: 2, GuildID: 1, Type: discord.GuildForum}, false); err != nil {
			t.Fatal(err)
		}

		h := handler.New()
		s := NewState(&state.State{Cabinet: cabinet, Handler: handler.New()}, h)

		ready := &gateway.ReadyEvent{User: discord.User{ID: 4}}
		ready.ReadStates = []gateway.ReadState{{ChannelID: 2, LastMessageID: 3, MentionCount: 1}}
		for _, event := range append([]any{ready}, events...) {
			h.Call(event)
		}

		return s
	}

	created := func(ownerID discord.UserID, newlyCreated bool) *gateway.ThreadCreateEvent {
		return &gateway.ThreadCreateEvent{Channel: discord.Channel{ID: 5, ParentID: 2, OwnerID: ownerID}, NewlyCreated: newlyCreated}
	}

	t.Run("a deleted channel or thread leaves no read state", func(t *testing.T) {
		for _, event := range []any{
			&gateway.ChannelDeleteEvent{Channel: discord.Channel{ID: 2}},
			&gateway.ThreadDeleteEvent{ID: 2},
		} {
			if got := read(t, event).ReadState(2); got != nil {
				t.Fatalf("%T: read state = %+v, want none", event, got)
			}
		}
	})

	for name, tt := range map[string]struct {
		event *gateway.ThreadCreateEvent
		want  discord.MessageID
	}{
		"creating a thread in a forum reads the forum up to it":    {created(4, true), 5},
		"a thread someone else created leaves the forum as it was": {created(9, true), 3},
		"being added to a thread leaves the forum as it was":       {created(4, false), 3},
	} {
		t.Run(name, func(t *testing.T) {
			if got := read(t, tt.event).ReadState(2).LastMessageID; got != tt.want {
				t.Fatalf("LastMessageID = %d, want %d", got, tt.want)
			}
		})
	}
}
