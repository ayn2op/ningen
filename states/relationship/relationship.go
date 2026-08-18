package relationship

import (
	"slices"
	"sync"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/arikawa/v3/state/store"
	"github.com/ayn2op/ningen/v3/handlerrepo"
)

type State struct {
	mutex         sync.RWMutex
	relationships map[discord.UserID]discord.Relationship
}

func NewState(store store.PresenceStore, r handlerrepo.AddHandler) *State {
	state := &State{
		relationships: map[discord.UserID]discord.Relationship{},
	}

	r.AddSyncHandler(func(r *gateway.ReadyEvent) {
		state.mutex.Lock()
		defer state.mutex.Unlock()

		clear(state.relationships)

		for _, rl := range r.Relationships {
			state.relationships[rl.UserID] = rl

			if rl.User.ID == rl.UserID {
				// Update our local presence state.
				presence, _ := store.Presence(0, rl.UserID)
				if presence != nil {
					presence.User = rl.User
				} else {
					presence = &discord.Presence{User: rl.User}
				}
				store.PresenceSet(0, presence, true)
			}
		}
	})

	r.AddSyncHandler(func(add *gateway.RelationshipAddEvent) {
		state.mutex.Lock()
		defer state.mutex.Unlock()

		state.relationships[add.UserID] = add.Relationship
	})

	r.AddSyncHandler(func(rem *gateway.RelationshipRemoveEvent) {
		state.mutex.Lock()
		defer state.mutex.Unlock()

		delete(state.relationships, rem.UserID)
	})

	return state
}

func (r *State) Each(fn func(discord.UserID, discord.RelationshipType) (stop bool)) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	for userID, relationship := range r.relationships {
		if fn(userID, relationship.Type) {
			return
		}
	}
}

// RelationshipType returns the relationship type for the given user, or 0 if
// there is none.
func (r *State) RelationshipType(userID discord.UserID) discord.RelationshipType {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.relationships[userID].Type
}

// Relationship returns the relationship type for the given user, or 0 if there
// is none.
//
// Deprecated: Use RelationshipType instead.
func (r *State) Relationship(userID discord.UserID) discord.RelationshipType {
	return r.RelationshipType(userID)
}

// FullRelationship returns the full Relationship for the given user. The second
// return value is false if no relationship exists.
func (r *State) FullRelationship(userID discord.UserID) (discord.Relationship, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	rel, ok := r.relationships[userID]
	return rel, ok
}

// IsBlocked returns if the user is blocked.
func (r *State) IsBlocked(userID discord.UserID) bool {
	return r.RelationshipType(userID) == discord.BlockedRelationship
}

// BlockedUserIDs returns all blocked users.
func (r *State) BlockedUserIDs() []discord.UserID {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	userIDs := make([]discord.UserID, 0, len(r.relationships))
	for uID, relationship := range r.relationships {
		if relationship.Type != discord.BlockedRelationship {
			continue
		}
		userIDs = append(userIDs, uID)
	}

	slices.Sort(userIDs)

	return userIDs
}
