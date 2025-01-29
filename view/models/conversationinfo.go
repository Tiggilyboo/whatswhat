package models

import (
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type ConversationMemberInfo struct {
	types.ContactInfo
	JID types.JID
}

type ConversationInfo struct {
	ChatJID  types.JID
	Name     string
	Members  []ConversationMemberInfo
	ReadOnly bool
}

func GetConversationInfo(client *whatsmeow.Client, chatJID types.JID) (*ConversationInfo, error) {
	switch chatJID.Server {
	case types.DefaultUserServer:
		contacts, err := client.Store.Contacts.GetAllContacts()
		if err != nil {
			return nil, err
		}

		members := make([]ConversationMemberInfo, 2)
		otherContact, ok := contacts[chatJID]
		if !ok {
			return nil, fmt.Errorf("Unable to find other contact in chat: %s", chatJID)
		}
		members[0] = ConversationMemberInfo{
			ContactInfo: otherContact,
			JID:         chatJID,
		}
		currentContact, ok := contacts[*client.Store.ID]
		if !ok {
			return nil, fmt.Errorf("Unable to find current user contact in chat: %s", chatJID)
		}
		members[1] = ConversationMemberInfo{
			ContactInfo: currentContact,
			JID:         *client.Store.ID,
		}
		chatName := otherContact.FullName

		return &ConversationInfo{
			ChatJID:  chatJID,
			Name:     chatName,
			Members:  members,
			ReadOnly: false,
		}, nil

	case types.NewsletterServer:
		info, err := client.GetNewsletterInfo(chatJID)
		if err != nil {
			return nil, err
		}
		members := make([]ConversationMemberInfo, 0)

		return &ConversationInfo{
			ChatJID:  info.ID,
			Name:     info.ThreadMeta.Name.Text,
			Members:  members,
			ReadOnly: true,
		}, nil

	case types.GroupServer:
		info, err := client.GetGroupInfo(chatJID)
		if err != nil {
			return nil, err
		}
		members := make([]ConversationMemberInfo, len(info.Participants))
		groupParticipants := info.Participants
		for _, p := range groupParticipants {
			memberContact, err := client.Store.Contacts.GetContact(p.JID)
			if err != nil {
				return nil, err
			}

			members = append(members, ConversationMemberInfo{
				ContactInfo: memberContact,
				JID:         p.JID,
			})
		}

		return &ConversationInfo{
			ChatJID:  info.JID,
			Name:     info.Name,
			Members:  members,
			ReadOnly: false,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported server %s", chatJID.Server)
	}
}
