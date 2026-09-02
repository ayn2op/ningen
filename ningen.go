// Package ningen contains a set of helpful functions and packages to aid in
// making a Discord client.
package ningen

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ayn2op/arikawa/v3/api"
	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/arikawa/v3/state"
	"github.com/ayn2op/arikawa/v3/utils/handler"
	"github.com/ayn2op/arikawa/v3/utils/httputil"
	"github.com/ayn2op/ningen/v3/nstore"
	"github.com/ayn2op/ningen/v3/states/emoji"
	"github.com/ayn2op/ningen/v3/states/guild"
	"github.com/ayn2op/ningen/v3/states/member"
	"github.com/ayn2op/ningen/v3/states/mute"
	"github.com/ayn2op/ningen/v3/states/note"
	"github.com/ayn2op/ningen/v3/states/read"
	"github.com/ayn2op/ningen/v3/states/relationship"
	"github.com/ayn2op/ningen/v3/states/summary"
	"github.com/ayn2op/ningen/v3/states/thread"
)

var cancelledCtx context.Context

func init() {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledCtx = c
}

type State struct {
	*state.State
	*handler.Handler

	// Custom Cabinet values.
	MemberStore   *nstore.MemberStore
	PresenceStore *nstore.PresenceStore

	// Custom State values.
	NoteState         *note.State
	ReadState         *read.State
	MutedState        *mute.State
	GuildState        *guild.State
	EmojiState        *emoji.State
	MemberState       *member.State
	ThreadState       *thread.State
	SummaryState      *summary.State
	RelationshipState *relationship.State

	oldCtx context.Context
}

// New creates a new ningen state from the given token and the default
// identifier.
func New(token string) *State {
	id := gateway.DefaultIdentifier(token)
	id.Capabilities = gateway.LazyUserNotes | gateway.VersionedReadStates | gateway.VersionedUserGuildSetttings | gateway.DedupeUserObjects | gateway.PrioritizedReadyPayload | gateway.MultipleGuildExperimentPopulations | gateway.NonChannelReadStates
	return NewWithIdentifier(id)
}

// NewWithIdentifier creates a new ningen state from the given identifier.
func NewWithIdentifier(id gateway.Identifier) *State {
	return FromState(state.NewWithIdentifier(id))
}

// FromState wraps a normal state.
func FromState(s *state.State) *State {
	state := &State{
		State:   s,
		Handler: handler.New(),
	}

	state.MemberStore = nstore.NewMemberStore()
	state.PresenceStore = nstore.NewPresenceStore()

	state.Cabinet.MemberStore = state.MemberStore
	state.Cabinet.PresenceStore = state.PresenceStore

	prehandler := s.Handler
	// Give our local states the synchronous prehandler.
	state.NoteState = note.NewState(s, prehandler)
	state.ReadState = read.NewState(s, prehandler)
	state.MutedState = mute.NewState(s.Cabinet, prehandler)
	state.GuildState = guild.NewState(prehandler)
	state.EmojiState = emoji.NewState(s.Cabinet)
	state.MemberState = member.NewState(s, prehandler)
	state.ThreadState = thread.NewState(s, prehandler)
	state.SummaryState = summary.NewState(s, prehandler)
	state.RelationshipState = relationship.NewState(s.Cabinet, prehandler)

	s.AddSyncHandler(func(v gateway.Event) {
		switch v := v.(type) {
		case *gateway.SessionsReplaceEvent:
			me, _ := s.Me()
			if me == nil {
				break
			}

			s.PresenceSet(0, joinSession(*me, v), true)

		case *gateway.UserSettingsUpdateEvent:
			me, _ := s.Me()
			if me == nil {
				break
			}

			p, _ := s.PresenceStore.Presence(0, me.ID)
			if p != nil {
				new := *p
				new.Status = v.Status

				if v.CustomStatus != nil {
					customActivity := discord.Activity{
						Name: v.CustomStatus.Text,
					}

					if v.CustomStatus.EmojiName != "" {
						customActivity.Emoji = &discord.Emoji{
							ID:   v.CustomStatus.EmojiID,
							Name: v.CustomStatus.EmojiName,
						}
					}

					new.Activities = slices.Clone(new.Activities)
					for i, activity := range new.Activities {
						if activity.Type == discord.CustomActivity {
							new.Activities[i] = customActivity
							goto found
						}
					}
					new.Activities = append(new.Activities, customActivity)
				found:
				}

				s.PresenceSet(p.GuildID, &new, true)
			}

		}

		// Call the external handler after we're done. This handler is
		// asynchronuos, or at least it should be.
		state.Handler.Call(v)
	})

	return state
}

// WithContext returns State with the given context.
func (s *State) WithContext(ctx context.Context) *State {
	cpy := *s
	cpy.State = cpy.State.WithContext(ctx)
	return &cpy
}

// Offline returns an offline version of the state.
func (s *State) Offline() *State {
	oldCtx := s.Context()
	cpy := s.WithContext(cancelledCtx)
	cpy.oldCtx = oldCtx
	return cpy
}

// Online returns an online state. If the state is already online, then it
// returns itself.
func (s *State) Online() *State {
	if s.oldCtx == nil {
		return s
	}
	online := s.WithContext(s.oldCtx)
	online.oldCtx = nil
	return online
}

// MessageMentionFlags is the resulting flag of a MessageMentions check. If it's
// 0, then absolutely no mentions are done, otherwise non-0 is returned.
type MessageMentionFlags uint8

const (
	// MessageMentions is when a message mentions the user either by tagging
	// that user or a role that the user is in.
	MessageMentions MessageMentionFlags = 1 << iota
	// MessageNotifies is when the message should also send a visible
	// notification.
	MessageNotifies
)

// Has returns true if other is in f.
func (f MessageMentionFlags) Has(other MessageMentionFlags) bool {
	return f&other == other
}

// Status returns the user's presence status. Use to check if notifications
// should be sent.
func (s *State) Status() discord.Status {
	me, _ := s.Cabinet.Me()
	if me == nil {
		return discord.OfflineStatus
	}

	if p, _ := s.PresenceStore.Presence(0, me.ID); p != nil {
		return p.Status
	}

	return discord.OfflineStatus
}

// MessageMentions returns true if the given message mentions the current user.
func (s *State) MessageMentions(msg *discord.Message) MessageMentionFlags {
	me, _ := s.Cabinet.Me()
	if me == nil {
		return 0
	}

	// Ignore own messages.
	if msg.Author.ID == me.ID {
		return 0
	}

	// Ignore messages from blocked users.
	if s.UserIsBlocked(msg.Author.ID) {
		return 0
	}

	var mutedGuild gateway.UserGuildSetting

	// If there's guild:
	if msg.GuildID.IsValid() {
		mutedGuild = s.MutedState.GuildSettings(msg.GuildID)

		// We're only checking mutes and suppressions, as channels don't
		// have these. Whatever channels have will override guilds.

		// @everyone mentions still work if the guild is muted and @everyone
		// is not suppressed.
		if msg.MentionEveryone && !mutedGuild.SuppressEveryone {
			return MessageMentions | MessageNotifies
		}

		// TODO: roles
	}

	var flags MessageMentionFlags
	if messageMentions(msg, me.ID) {
		flags = MessageMentions
	}

	// Check channel settings. Channel settings override guilds.
	mutedCh := s.MutedState.ChannelOverrides(msg.ChannelID)

	switch mutedCh.Notifications {
	case gateway.NoNotifications:
		// No notifications are allowed whatsoever.
		return 0

	case gateway.AllNotifications:
		if mutedCh.Muted {
			return flags
		}

	case gateway.OnlyMentions:
		// If mentions are allowed. We return early because this overrides
		// the guild settings, even if Guild wants all messages.
		if flags != 0 {
			flags |= MessageNotifies
		}
		return flags
	}

	if msg.GuildID.IsValid() {
		switch mutedGuild.Notifications {
		case gateway.NoNotifications:
			// No notifications are allowed whatsoever.
			return 0

		case gateway.AllNotifications:
			if !mutedGuild.Muted {
				// All messages trigger notification if not muted.
				flags |= MessageNotifies
			}
			return flags

		case gateway.OnlyMentions:
			if flags != 0 {
				// If mentioned, will always notify.
				flags |= MessageNotifies
			}
			return flags
		}
	}

	// Is this from a DM? TODO: get a better check.
	if ch, err := s.Cabinet.Channel(msg.ChannelID); err == nil {
		// True if the message is from DM or group.
		if ch.Type == discord.DirectMessage || ch.Type == discord.GroupDM {
			return flags | MessageNotifies
		}
	}

	return flags
}

func messageMentions(msg *discord.Message, uID discord.UserID) bool {
	for _, user := range msg.Mentions {
		if user.ID == uID {
			return true
		}
	}
	return false
}

func joinSession(me discord.User, r *gateway.SessionsReplaceEvent) *discord.Presence {
	ses := *r

	var status discord.Status
	var activities []discord.Activity

	for _, presence := range slices.Backward(ses) {
		if presence.Status != "" {
			status = presence.Status
		}

		activities = append(activities, presence.Activities...)
	}

	return &discord.Presence{
		User:       me,
		Status:     status,
		Activities: activities,
	}
}

// PrivateChannels returns the sorted list of private channels from the state.
func (s *State) PrivateChannels() ([]discord.Channel, error) {
	c, err := s.State.PrivateChannels()
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(c, func(a, b discord.Channel) int {
		return cmp.Compare(b.LastMessageID, a.LastMessageID)
	})

	return c, nil
}

// Channels returns a list of visible channels. Empty categories are
// automatically filtered out.
func (s *State) Channels(guildID discord.GuildID, allowedTypes []discord.ChannelType) ([]discord.Channel, error) {
	// I have fully given up on life.
	var allowedMap [64]bool
	for _, t := range allowedTypes {
		allowedMap[t] = true
	}

	chs, err := s.State.Channels(guildID)
	if err != nil {
		return nil, err
	}

	filtered := chs[:0]

	// Filter out channels we can't see.
	for _, ch := range chs {
		if !allowedMap[ch.Type] {
			continue
		}

		// Only check if the channel is not a category, since we're filtering
		// out empty categories anyway.
		if ch.Type != discord.GuildCategory {
			if !s.HasPermissions(ch.ID, discord.PermissionViewChannel) {
				continue
			}
		}

		filtered = append(filtered, ch)
	}

	chs = filtered

	categories := make(map[discord.ChannelID]int, 10)
	// Initialize the category map.
	for _, ch := range chs {
		if ch.Type == discord.GuildCategory {
			categories[ch.ID] = 0
		}
	}

	// Count all channels within categories.
	for _, ch := range chs {
		_, ok := categories[ch.ParentID]
		if ok {
			categories[ch.ParentID]++
		}
	}

	filtered = chs[:0]

	// Filter again but exclude all categories with no channels.
	for _, ch := range chs {
		if count, ok := categories[ch.ID]; ok && count == 0 {
			continue
		}
		filtered = append(filtered, ch)
	}

	return filtered, nil
}

// NoPermissionError is returned by AssertPermissions if the user lacks
// the requested permissions.
type NoPermissionError struct {
	Has    discord.Permissions
	Wanted discord.Permissions
}

// Error implemenets error.
func (err *NoPermissionError) Error() string {
	return "user is missing permission"
}

// HasPermissions returns true if AssertPermissions returns a nil error.
func (s *State) HasPermissions(chID discord.ChannelID, perms discord.Permissions) bool {
	return s.AssertPermissions(chID, perms) == nil
}

// AssertPermissions asserts that the current user has the given permissions in
// the given channel. If the assertion fails, a NoPermissionError might be
// returned.
func (s *State) AssertPermissions(chID discord.ChannelID, perms discord.Permissions) error {
	me, err := s.Me()
	if err != nil {
		return fmt.Errorf("cannot get current user information: %w", err)
	}

	p, err := s.Permissions(chID, me.ID)
	if err != nil {
		return fmt.Errorf("cannot get permissions: %w", err)
	}

	if !p.Has(perms) {
		return &NoPermissionError{
			Has:    p,
			Wanted: perms,
		}
	}

	return nil
}

// LastMessage returns the last message ID in the given channel.
func (r *State) LastMessage(chID discord.ChannelID) discord.MessageID {
	ch, _ := r.Cabinet.Channel(chID)
	// Discord uses this cursor for unread comparisons even when it no longer
	// points to an existing message.
	if ch != nil && ch.LastMessageID.IsValid() {
		return ch.LastMessageID
	}

	msgs, _ := r.Cabinet.Messages(chID)
	if len(msgs) > 0 {
		return msgs[0].ID
	}

	return 0
}

// UnreadIndication indicates the channel as either unread, mentioned (which
// implies unread) or neither.
type UnreadIndication uint8

const (
	ChannelRead UnreadIndication = iota
	ChannelUnread
	ChannelMentioned
)

// UnreadOpts are options for the Unread function.
type UnreadOpts struct {
	// IncludeMutedCategories includes channels in muted categories.
	// This means that if a category is muted, but a channel within it is not,
	// the channel will still be considered unread.
	IncludeMutedCategories bool
}

// ChannelIsUnread returns true if the channel with the given ID has unread
// messages.
func (r *State) ChannelIsUnread(chID discord.ChannelID, opts UnreadOpts) UnreadIndication {
	state := r.ReadState.ReadState(chID)
	if state == nil || !state.LastMessageID.IsValid() {
		return ChannelRead
	}

	// Mentions override mutes.
	if state.MentionCount > 0 {
		return ChannelMentioned
	}

	if r.ChannelIsMuted(chID, opts) {
		return ChannelRead
	}

	lastMsgID := r.LastMessage(chID)
	if !lastMsgID.IsValid() {
		return ChannelRead
	}
	if state.LastMessageID >= lastMsgID {
		return ChannelRead
	}

	if !r.HasPermissions(chID, discord.PermissionViewChannel) {
		return ChannelRead
	}

	return ChannelUnread
}

// GuildUnreadOpts are options for the GuildIsUnread function.
type GuildUnreadOpts struct {
	UnreadOpts
	// Types is a list of channel types to check for unread messages.
	Types []discord.ChannelType
}

// GuildIsUnread returns true if the guild contains unread channels.
func (r *State) GuildIsUnread(guildID discord.GuildID, opts GuildUnreadOpts) UnreadIndication {
	chs, err := r.Cabinet.Channels(guildID)
	if err != nil {
		return ChannelRead
	}

	var typeMap [128]bool
	for _, typ := range opts.Types {
		typeMap[typ] = true
	}

	ind := ChannelRead
	for _, ch := range chs {
		if opts.Types != nil && !typeMap[ch.Type] {
			continue
		}
		if s := r.ChannelIsUnread(ch.ID, opts.UnreadOpts); s == ChannelMentioned {
			return s
		} else if s > ind {
			ind = s
		}
	}

	if isMuted := r.MutedState.Guild(guildID, false); isMuted {
		// Only show mentions for muted guilds.
		if ind != ChannelMentioned {
			return ChannelRead
		}
	}

	return ind
}

// ChanneCountUnreads returns the number of unread messages in the channel.
func (s *State) ChannelCountUnreads(chID discord.ChannelID, opts UnreadOpts) int {
	var unread int

	// Grab our known messages so we can count the unread ones.
	msgs, _ := s.Cabinet.Messages(chID)

	readState := s.ReadState.ReadState(chID)
	if readState == nil || !readState.LastMessageID.IsValid() {
		return 0
	}

	if msgs != nil {
		// We've seen this channel before, so we'll count (if we can )the unread
		// messages from the last read message.
		for _, msg := range msgs {
			if msg.ID > readState.LastMessageID {
				unread++
			} else {
				// We've reached the last read message, so we can stop counting.
				break
			}
		}
	} else if unread == 0 && s.ChannelIsUnread(chID, opts) != ChannelRead {
		unread = 1
	}

	return unread
}

// SetStatus sets the current user's status and presence.
func (r *State) SetStatus(status discord.Status, custom *gateway.CustomUserStatus, activities ...discord.Activity) error {
	me, _ := r.Me()

	cmd := gateway.UpdatePresenceCommand{
		Status:     status,
		Activities: activities,
	}

	if custom != nil {
		customActivity := discord.Activity{
			Name:  "Custom Status",
			Type:  discord.CustomActivity,
			State: custom.Text,
		}

		if custom.EmojiName != "" {
			customActivity.Emoji = &discord.Emoji{
				ID:   custom.EmojiID,
				Name: custom.EmojiName,
			}
		}

		activities = slices.Clone(activities)
		activities = append(activities, customActivity)
	}

	if p, _ := r.PresenceStore.Presence(0, me.ID); p != nil {
		if status == "" && p.Status != "" {
			cmd.Status = p.Status
		}
		if activities == nil && p.Activities != nil {
			cmd.Activities = p.Activities
		}
	}

	if err := r.SendGateway(r.Context(), &cmd); err != nil {
		return fmt.Errorf("cannot update gateway: %w", err)
	}

	// Keep this the same as gateway.UserSettings.
	patchSettings := map[string]any{"status": status}
	if custom != nil {
		patchSettings["custom_status"] = custom
	}

	if err := r.FastRequest("PATCH", api.EndpointMe+"/settings", httputil.WithJSONBody(patchSettings)); err != nil {
		return fmt.Errorf("cannot update user settings API: %w", err)
	}
	return nil
}

// SetAFK sets the current user's AFK status. If the user is AFK, then they will
// be receiving push notifications. The `since` parameter is the time that the
// user last interacted with Discord. The status is automatically set to idle.
func (r *State) SetAFK(afk bool, since time.Time) error {
	if afk {
		return r.SendGateway(r.Context(), &gateway.UpdatePresenceCommand{
			Status:     discord.IdleStatus,
			Activities: []discord.Activity{},
			Since:      discord.TimeToMilliseconds(since),
			AFK:        true,
		})
	}

	me, err := r.Me()
	if err != nil {
		return fmt.Errorf("cannot get current user information: %w", err)
	}

	// Try to restore the user's status.
	presences, err := r.State.Presence(0, me.ID)
	if err != nil {
		return fmt.Errorf("cannot get presences: %w", err)
	}

	return r.SendGateway(r.Context(), &gateway.UpdatePresenceCommand{
		Status:     presences.Status,
		Activities: presences.Activities,
		Since:      0,
		AFK:        false,
	})
}

// UserIsBlocked returns true if the user with the given ID is blocked by the
// current user.
func (r *State) UserIsBlocked(uID discord.UserID) bool {
	return r.RelationshipState.IsBlocked(uID)
}

// ChannelIsMuted returns true if the channel with the given ID is muted or if
// it's in a category that's muted.
func (r *State) ChannelIsMuted(chID discord.ChannelID, opts UnreadOpts) bool {
	// If the channel is configured to be muted, then it's muted.
	if r.MutedState.Channel(chID) {
		return true
	}

	c, err := r.Cabinet.Channel(chID)
	if err != nil {
		slog.Warn(
			"cannot get channel to check if it's muted",
			"module", "ningen",
			"channel_id", chID,
			"err", err)
		return false
	}

	// Is the channel a thread? If so, check if the thread is joined.
	switch c.Type {
	case discord.GuildPublicThread, discord.GuildPrivateThread:
		if !r.ThreadState.ThreadIsJoined(c.ID) {
			return true
		}
	}

	// If the channel is in a category, then check if the category is muted.
	if !opts.IncludeMutedCategories && c.ParentID.IsValid() {
		return r.ChannelIsMuted(c.ParentID, opts)
	}

	return false
}
