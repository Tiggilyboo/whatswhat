package models

import (
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type ConversationMemberModel struct {
	types.ContactInfo
	JID types.JID
}

type ConversationModel struct {
	ChatJID              types.JID
	Name                 string
	Members              []ConversationMemberModel
	UnreadCount          uint
	LastMessageTimestamp time.Time
}

func GetConversationModel(client *whatsmeow.Client, contacts map[types.JID]types.ContactInfo, chatJID types.JID, chatName string, unread uint, lastMessageTime time.Time, detailed bool) (*ConversationModel, error) {
	var members []ConversationMemberModel
	switch chatJID.Server {
	case types.DefaultUserServer:
		var members []ConversationMemberModel
		if detailed {
			members = make([]ConversationMemberModel, 2)
			otherContact, ok := contacts[chatJID.ToNonAD()]
			if !ok {
				return nil, fmt.Errorf("Unable to find other contact in chat: %s", chatJID)
			}
			members[0] = ConversationMemberModel{
				ContactInfo: otherContact,
				JID:         chatJID,
			}
			currentContact, ok := contacts[client.Store.ID.ToNonAD()]
			if !ok {
				return nil, fmt.Errorf("Unable to find current user contact in chat: %s", chatJID)
			}
			members[1] = ConversationMemberModel{
				ContactInfo: currentContact,
				JID:         *client.Store.ID,
			}
		}

	case types.NewsletterServer:
		if detailed {
			info, err := client.GetNewsletterInfo(chatJID)
			if err != nil {
				return nil, err
			}
			chatName = info.ThreadMeta.Name.Text
		}
		members = make([]ConversationMemberModel, 0)

	case types.GroupServer:

		if detailed {
			info, err := client.GetGroupInfo(chatJID)
			if err != nil {
				return nil, err
			}
			members := make([]ConversationMemberModel, len(info.Participants))
			groupParticipants := info.Participants
			for _, p := range groupParticipants {
				memberContact, ok := contacts[p.JID.ToNonAD()]
				if !ok {
					memberContact, err = client.Store.Contacts.GetContact(p.JID)
					if err != nil {
						fmt.Printf("Unable to find group '%s' participant %s\n", info.Name, p.DisplayName)
						continue
					}
				}

				members = append(members, ConversationMemberModel{
					ContactInfo: memberContact,
					JID:         p.JID,
				})
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
