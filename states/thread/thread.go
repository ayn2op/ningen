package thread

import (
	"slices"
	"sync"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/arikawa/v3/state"
	"github.com/ayn2op/arikawa/v3/state/store"
	"github.com/ayn2op/arikawa/v3/utils/handler"
)

// State contains additional thread states that are not in the built-in state
// cache.
type State struct {
	state   *state.State
	cabinet *store.Cabinet

	membersMu sync.RWMutex
	// members holds the current user's member of each thread they have joined.
	members map[discord.ChannelID]discord.ThreadMember
}

func NewState(state *state.State, h *handler.Handler) *State {
	s := &State{
		state:   state,
		cabinet: state.Cabinet,
		members: make(map[discord.ChannelID]discord.ThreadMember),
	}

	var userID discord.UserID

	h.AddSyncHandler(func(ev *gateway.ReadyEvent) {
		userID = ev.User.ID

		s.membersMu.Lock()
		defer s.membersMu.Unlock()
		clear(s.members)

		for _, guild := range ev.Guilds {
			s.join(guild.Threads)
		}
	})

	h.AddSyncHandler(func(ev *gateway.GuildCreateEvent) {
		s.membersMu.Lock()
		defer s.membersMu.Unlock()

		s.join(ev.Threads)
	})

	h.AddSyncHandler(func(ev *gateway.ThreadListSyncEvent) {
		s.membersMu.Lock()
		defer s.membersMu.Unlock()

		// The event holds the member of every thread in it that we have
		// joined, so we have not joined the others.
		for _, thread := range ev.Threads {
			delete(s.members, thread.ID)
		}

		for _, member := range ev.Members {
			s.members[member.ID] = member
		}
	})

	h.AddSyncHandler(func(ev *gateway.ThreadCreateEvent) {
		// The member is included when we are added to a thread.
		if ev.ThreadMember == nil {
			return
		}

		s.membersMu.Lock()
		s.members[ev.ID] = *ev.ThreadMember
		s.membersMu.Unlock()
	})

	h.AddSyncHandler(func(ev *gateway.ThreadDeleteEvent) {
		s.membersMu.Lock()
		delete(s.members, ev.ID)
		s.membersMu.Unlock()
	})

	h.AddSyncHandler(func(ev *gateway.ThreadMembersUpdateEvent) {
		for _, member := range ev.AddedMembers {
			if member.UserID == userID {
				// We joined a thread.
				s.membersMu.Lock()
				s.members[ev.ID] = member
				s.membersMu.Unlock()
				return
			}
		}

		if slices.Contains(ev.RemovedMemberIDs, userID) {
			// We left a thread.
			s.membersMu.Lock()
			delete(s.members, ev.ID)
			s.membersMu.Unlock()
			return
		}
	})

	h.AddSyncHandler(func(ev *gateway.ThreadMemberUpdateEvent) {
		if ev.UserID != userID {
			return
		}

		s.membersMu.Lock()
		s.members[ev.ID] = ev.ThreadMember
		s.membersMu.Unlock()
	})

	return s
}

// join records the threads, which are ones the current user has joined, with
// the member each holds. It must be called with membersMu held.
func (s *State) join(threads []discord.Channel) {
	for _, thread := range threads {
		var member discord.ThreadMember
		if thread.ThreadMember != nil {
			member = *thread.ThreadMember
		}

		s.members[thread.ID] = member
	}
}

// ThreadMember returns the current user's member of the given thread, and
// whether they have joined it.
func (s *State) ThreadMember(id discord.ChannelID) (discord.ThreadMember, bool) {
	s.membersMu.RLock()
	defer s.membersMu.RUnlock()

	member, ok := s.members[id]
	return member, ok
}

// ThreadIsJoined returns whether the current user is joined to the given
// thread.
func (s *State) ThreadIsJoined(id discord.ChannelID) bool {
	_, ok := s.ThreadMember(id)
	return ok
}
