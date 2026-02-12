package bootstrap

import (
	"testing"

	"github.com/uncord-chat/uncord-protocol/permissions"
)

func TestDefaultEveryonePermissions(t *testing.T) {
	// Permissions that MUST be set on @everyone
	required := []struct {
		perm permissions.Permission
		name string
	}{
		{permissions.ViewChannels, "ViewChannels"},
		{permissions.SendMessages, "SendMessages"},
		{permissions.ReadMessageHistory, "ReadMessageHistory"},
		{permissions.AddReactions, "AddReactions"},
		{permissions.CreateInvites, "CreateInvites"},
		{permissions.ChangeNicknames, "ChangeNicknames"},
		{permissions.VoiceConnect, "VoiceConnect"},
		{permissions.VoiceSpeak, "VoiceSpeak"},
		{permissions.VoicePTT, "VoicePTT"},
	}

	for _, tt := range required {
		if !DefaultEveryonePermissions.Has(tt.perm) {
			t.Errorf("DefaultEveryonePermissions missing %s", tt.name)
		}
	}

	// Privileged permissions that MUST NOT be set on @everyone
	forbidden := []struct {
		perm permissions.Permission
		name string
	}{
		{permissions.ManageChannels, "ManageChannels"},
		{permissions.ManageRoles, "ManageRoles"},
		{permissions.ManageServer, "ManageServer"},
		{permissions.KickMembers, "KickMembers"},
		{permissions.BanMembers, "BanMembers"},
		{permissions.ManageMessages, "ManageMessages"},
		{permissions.MentionEveryone, "MentionEveryone"},
		{permissions.ManageWebhooks, "ManageWebhooks"},
		{permissions.ViewAuditLog, "ViewAuditLog"},
	}

	for _, tt := range forbidden {
		if DefaultEveryonePermissions.Has(tt.perm) {
			t.Errorf("DefaultEveryonePermissions should not include %s", tt.name)
		}
	}
}
