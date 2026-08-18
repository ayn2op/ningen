package emoji

import (
	"slices"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/state/store"
	"github.com/pkg/errors"
)

type State struct {
	cab *store.Cabinet
}

type Guild struct {
	discord.Guild
	Emojis []discord.Emoji
}

func NewState(cab *store.Cabinet) *State {
	return &State{
		cab: cab,
	}
}

// HasNitro returns true if the current user has Nitro.
func (s *State) HasNitro() bool {
	u, err := s.cab.Me()
	return err == nil && u.Nitro != discord.NoUserNitro
}

// ForGuild returns all emojis if the user has Nitro, else only non-animated
// emojis from the given guild.
func (s *State) ForGuild(guildID discord.GuildID) ([]Guild, error) {
	if s.HasNitro() {
		emojis, err := s.AllEmojis()
		if err != nil {
			return nil, err
		}

		PutGuildFirst(emojis, guildID)
		return emojis, nil
	}

	// User doesn't have Nitro, so only non-GIF guild emojis are available:

	// If we don't have a guildID, return nothing.
	if !guildID.IsValid() {
		return nil, nil
	}

	g, err := s.cab.Guild(guildID)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get guild")
	}

	emojis, err := s.cab.Emojis(guildID)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get emojis")
	}

	filtered := emojis[:0]

	for _, e := range emojis {
		if !e.Animated {
			filtered = append(filtered, e)
		}
	}

	if len(filtered) == 0 {
		// No emojis.
		return nil, nil
	}

	return []Guild{{
		Guild:  *g,
		Emojis: emojis,
	}}, nil
}

// AllEmojis returns the
func (s *State) AllEmojis() ([]Guild, error) {
	// User has Nitro, grab all emojis.
	guilds, err := s.cab.Guilds()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get guilds")
	}

	emojis := make([]Guild, 0, len(guilds))

	for _, g := range guilds {
		if e, err := s.cab.Emojis(g.ID); err == nil {
			if len(e) == 0 {
				continue
			}

			emojis = append(emojis, Guild{
				Guild:  g,
				Emojis: e,
			})
		}
	}

	return emojis, nil
}

// PutGuildFirst puts the guild with the given ID first in the guilds list.
func PutGuildFirst(guilds []Guild, first discord.GuildID) {
	slices.SortStableFunc(guilds, func(a, b Guild) int {
		switch {
		case a.ID == first && b.ID != first:
			return -1
		case a.ID != first && b.ID == first:
			return 1
		default:
			return 0
		}
	})
}
