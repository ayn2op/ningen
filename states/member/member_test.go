package member

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"testing"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/gateway"
	"github.com/ayn2op/arikawa/v3/state"
)

type mockNingen struct {
	*state.State
	MemberState *State
}

func ningenFromState(s *state.State) *mockNingen {
	return &mockNingen{s, NewState(s, s)}
}

const (
	GuildID   = 0
	ChannelID = 0
)

func ExampleState_RequestMemberList() {
	s := state.New(os.Getenv("TOKEN"))

	// Replace with the actual ningen.FromState function.
	n := ningenFromState(s)

	updates := make(chan *gateway.GuildMemberListUpdateEvent, 1)
	n.AddHandler(updates)

	if err := n.Open(context.TODO()); err != nil {
		panic(err)
	}

	defer n.Close()

	for i := 0; ; i++ {
		c := n.MemberState.RequestMemberList(GuildID, ChannelID, i)
		if c == nil {
			break
		}

		<-updates
		log.Println("Received", i)
	}

	l, err := n.MemberState.GetMemberList(GuildID, ChannelID)
	if err != nil {
		panic(err)
	}

	l.ViewGroups(func(groups []gateway.GuildMemberListGroup) {
		for _, group := range groups {
			var name = group.ID
			if p, err := discord.ParseSnowflake(name); err == nil {
				r, err := s.Role(GuildID, discord.RoleID(p))
				if err != nil {
					log.Fatalln("Failed to get role:", err)
				}

				name = r.Name
			}

			fmt.Println("Group:", name, group.Count)
		}
	})

	l.ViewItems(func(items []gateway.GuildMemberListOpItem) {
		for i := 0; i < len(items); i += 100 {
			for j := 0; j < 99 && i+j < len(items); j++ {
				if ListItemIsNil(items[i+j]) {
					fmt.Print(" ")
				} else {
					fmt.Print("O")
				}
			}

			fmt.Println("|")
		}

		var firstNonNil = ListItemSeek(items, 100)
		fmt.Println("First non-nil past 100:", firstNonNil)
		fmt.Println("Above member:", items[firstNonNil].Member)

		fmt.Println("Last member:", items[len(items)-1].Member.User.Username)
	})
}

func TestComputeListID(t *testing.T) {
	perms := []discord.Overwrite{
		{Type: discord.OverwriteRole, ID: 361910177961738242, Deny: 1024, Allow: 0},
		{Type: discord.OverwriteRole, ID: 361919857836425217, Deny: 0, Allow: 117760},
		{Type: discord.OverwriteRole, ID: 532359766694035457, Deny: 0, Allow: 10240},
		{Type: discord.OverwriteRole, ID: 564702909519101952, Deny: 93184, Allow: 0},
		{Type: discord.OverwriteRole, ID: 578035907232530432, Deny: 2112, Allow: 0},
		{Type: discord.OverwriteRole, ID: 697931217521082455, Deny: 0, Allow: 1024},
	}

	if id := ComputeListID(perms); id != "3720633681" {
		t.Fatal("Unexpected ID:", id, "expected", "3720633681")
	}

	// The ID does not depend on the order of the overwrites.
	slices.Reverse(perms)
	if id := ComputeListID(perms); id != "3720633681" {
		t.Fatal("Unexpected ID for reversed overwrites:", id, "expected", "3720633681")
	}
}

func TestRequestMemberList(t *testing.T) {
	s := state.New("")
	s.Cabinet.ChannelSet(&discord.Channel{ID: 1, GuildID: 10}, false)
	s.Cabinet.ChannelSet(&discord.Channel{ID: 2, GuildID: 10}, false)
	m := NewState(s, s.Handler)
	// The gateway is closed, so the subscriptions fail to send.
	m.OnError = func(error) {}

	for _, tt := range []struct {
		channelID discord.ChannelID
		want      bool
	}{
		{1, true},
		{1, false},
		{2, true},
		// Returning to a channel subscribes to it again.
		{1, true},
	} {
		if got := m.RequestMemberList(10, tt.channelID, 0) != nil; got != tt.want {
			t.Fatalf("requested channel %d = %v, want %v", tt.channelID, got, tt.want)
		}
	}
}

func TestStateListID(t *testing.T) {
	const guildID = 10
	s := state.New("")
	s.Cabinet.RoleSet(guildID, &discord.Role{ID: guildID, Permissions: discord.PermissionViewChannel}, false)
	m := NewState(s, s.Handler)

	for _, tt := range []struct {
		name       string
		overwrites []discord.Overwrite
		want       string
	}{
		{"everyone allowed", []discord.Overwrite{{ID: guildID, Type: discord.OverwriteRole, Allow: discord.PermissionViewChannel}}, "everyone"},
		{"everyone denied", []discord.Overwrite{{ID: guildID, Type: discord.OverwriteRole, Deny: discord.PermissionViewChannel}}, ComputeListID([]discord.Overwrite{{ID: guildID, Deny: discord.PermissionViewChannel}})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.ListID(&discord.Channel{GuildID: guildID, Overwrites: tt.overwrites}); got != tt.want {
				t.Fatalf("ListID() = %q, want %q", got, tt.want)
			}
		})
	}
}
