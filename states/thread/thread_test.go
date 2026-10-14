package thread

import (
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/arikawa/v3/state"
	"github.com/ayn2op/arikawa/v3/state/store/defaultstore"
	"github.com/ayn2op/arikawa/v3/utils/handler"
)

func TestStateThreadMember(t *testing.T) {
	// joined returns the state of user 1, who is in thread 10, after events.
	joined := func(events ...any) *State {
		h := handler.New()
		s := NewState(&state.State{Cabinet: defaultstore.New()}, h)

		ready := &gateway.ReadyEvent{User: discord.User{ID: 1}}
		ready.Guilds = []gateway.GuildCreateEvent{{Threads: []discord.Channel{{ID: 10, ThreadMember: &discord.ThreadMember{Muted: true}}}}}
		for _, event := range append([]any{ready}, events...) {
			h.Call(event)
		}

		return s
	}

	t.Run("holds the member of a thread joined at the start", func(t *testing.T) {
		if member, ok := joined().ThreadMember(10); !ok || !member.Muted {
			t.Fatalf("member = %+v, %v, want a muted one", member, ok)
		}
	})

	t.Run("a sync says which of its threads are joined", func(t *testing.T) {
		s := joined(&gateway.ThreadListSyncEvent{
			Threads: []discord.Channel{{ID: 10}, {ID: 11}},
			Members: []discord.ThreadMember{{ID: 11}},
		})
		if s.ThreadIsJoined(10) || !s.ThreadIsJoined(11) {
			t.Fatalf("joined 10 = %v, 11 = %v, want only 11", s.ThreadIsJoined(10), s.ThreadIsJoined(11))
		}
	})

	t.Run("being added to a thread joins it", func(t *testing.T) {
		create := &gateway.ThreadCreateEvent{Channel: discord.Channel{ID: 12, ThreadMember: &discord.ThreadMember{}}}
		if !joined(create).ThreadIsJoined(12) {
			t.Fatal("the thread is not joined")
		}
	})

	t.Run("a deleted thread is forgotten", func(t *testing.T) {
		if joined(&gateway.ThreadDeleteEvent{ID: 10}).ThreadIsJoined(10) {
			t.Fatal("the thread is still joined")
		}
	})
}
