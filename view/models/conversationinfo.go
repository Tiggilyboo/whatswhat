package models

import (
	"fmt"
	"time"

	"github.com/tiggilyboo/whatswhat/db"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type ConversationMemberInfo struct {
	types.ContactInfo
	JID types.JID
}

type ConversationInfo struct {
	ChatJID              types.JID
	Name                 string
	Members              []ConversationMemberInfo
	Unread               bool
	LastMessageTimestamp time.Time
}

func GetConversationInfo(client *whatsmeow.Client, convo *db.Conversation, contacts map[types.JID]types.ContactInfo, detailed bool) (*ConversationInfo, error) {
	chatName := convo.Name
	unread := false
	if convo.MarkedAsUnread != nil {
		unread = *convo.MarkedAsUnread
	}
	var members []ConversationMemberInfo

	switch convo.ChatJID.Server {
	case types.DefaultUserServer:
		var members []ConversationMemberInfo
		if detailed {
			members = make([]ConversationMemberInfo, 2)
			otherContact, ok := contacts[convo.ChatJID.ToNonAD()]
			if !ok {
				return nil, fmt.Errorf("Unable to find other contact in chat: %s", convo.ChatJID)
			}
			members[0] = ConversationMemberInfo{
				ContactInfo: otherContact,
				JID:         convo.ChatJID,
			}
			currentContact, ok := contacts[client.Store.ID.ToNonAD()]
			if !ok {
				return nil, fmt.Errorf("Unable to find current user contact in chat: %s", convo.ChatJID)
			}
			members[1] = ConversationMemberInfo{
				ContactInfo: currentContact,
				JID:         *client.Store.ID,
			}
		}

	case types.NewsletterServer:
		if detailed {
			info, err := client.GetNewsletterInfo(convo.ChatJID)
			if err != nil {
				return nil, err
			}
			chatName = info.ThreadMeta.Name.Text
		}
		members = make([]ConversationMemberInfo, 0)

	case types.GroupServer:

		if detailed {
			info, err := client.GetGroupInfo(convo.ChatJID)
			if err != nil {
				return nil, err
			}
			members := make([]ConversationMemberInfo, len(info.Participants))
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

				members = append(members, ConversationMemberInfo{
					ContactInfo: memberContact,
					JID:         p.JID,
				})
			}
		}

	default:
		return nil, fmt.Errorf("unsupported server %s", convo.ChatJID.Server)
	}

	return &ConversationInfo{
		ChatJID:              convo.ChatJID,
		Name:                 chatName,
		Members:              members,
		Unread:               unread,
		LastMessageTimestamp: convo.LastMessageTimestamp,
	}, nil
}
