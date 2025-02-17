package models

import (
	"fmt"
	"time"

	"github.com/tiggilyboo/whatswhat/db"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type ConversationMemberModel struct {
	JID       types.JID
	FirstName string
	FullName  string
	PushName  string
}

type ConversationModel struct {
	ChatJID              types.JID
	Name                 string
	Members              []ConversationMemberModel
	UnreadCount          uint
	LastMessageTimestamp time.Time
}

func resolveMember(client *whatsmeow.Client, contacts map[types.JID]types.ContactInfo, pushNames map[types.JID]*db.PushName, contactJID types.JID) ConversationMemberModel {
	nonAdJid := contactJID.ToNonAD()
	var member *ConversationMemberModel

	if contactJID == *client.Store.ID {
		member = &ConversationMemberModel{
			JID:       contactJID,
			FirstName: client.Store.PushName,
			FullName:  client.Store.PushName,
			PushName:  client.Store.PushName,
		}
	} else if pushName, ok := pushNames[nonAdJid]; ok {
		member = &ConversationMemberModel{
			JID:       contactJID,
			FirstName: pushName.Name,
			FullName:  pushName.Name,
			PushName:  pushName.Name,
		}
	} else {
		otherContact, ok := contacts[nonAdJid]
		if !ok {
			otherContact, _ = client.Store.Contacts.GetContact(nonAdJid)
			ok = true
		}
		if ok {
			member = &ConversationMemberModel{
				JID:       contactJID,
				FirstName: otherContact.FirstName,
				FullName:  otherContact.FullName,
				PushName:  otherContact.PushName,
			}
		}

	}
	if member == nil {
		member = &ConversationMemberModel{
			JID:       contactJID,
			FirstName: contactJID.User,
			FullName:  contactJID.Server,
		}
	}

	return *member
}

func GetConversationModel(client *whatsmeow.Client, contacts map[types.JID]types.ContactInfo, pushNames map[types.JID]*db.PushName, chatJID types.JID, chatName string, unread uint, lastMessageTime time.Time, detailed bool) (*ConversationModel, error) {
	var members []ConversationMemberModel
	switch chatJID.Server {
	case types.DefaultUserServer:
		if detailed {
			members = make([]ConversationMemberModel, 2)
			members[0] = resolveMember(client, contacts, pushNames, chatJID)
			members[1] = resolveMember(client, contacts, pushNames, *client.Store.ID)
		}
	case types.NewsletterServer:
		if detailed {
			info, err := client.GetNewsletterInfo(chatJID)
			if err != nil {
				return nil, err
			}
			chatName = info.ThreadMeta.Name.Text
		}
		members = []ConversationMemberModel{}

	case types.GroupServer:

		if detailed {
			info, err := client.GetGroupInfo(chatJID)
			if err != nil {
				return nil, err
			}
			members := make([]ConversationMemberModel, len(info.Participants))
			groupParticipants := info.Participants
			for i, p := range groupParticipants {
				members[i] = resolveMember(client, contacts, pushNames, p.JID)
			}
		}

	default:
		return nil, fmt.Errorf("unsupported server %s", chatJID.Server)
	}

	return &ConversationModel{
		ChatJID:              chatJID,
		Name:                 chatName,
		Members:              members,
		UnreadCount:          unread,
		LastMessageTimestamp: lastMessageTime,
	}, nil
}
