package models

import (
	"fmt"
	"regexp"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/tiggilyboo/whatswhat/db"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type MessageType uint8

const (
	MessageTypeUnknown MessageType = iota
	MessageTypeReference
	MessageTypeLocation
	MessageTypeText
	MessageTypeImage
	MessageTypeAudio
	MessageTypeVideo
	MessageTypeDocument
)

var urlRegex = regexp.MustCompile(`(https?://[^\s<>"'()]+)`)

type MessageModel struct {
	ID           types.MessageID
	ReferencesID *string
	ChatJID      types.JID
	SenderJID    types.JID
	PushName     string
	IsFromMe     bool
	IsGroup      bool
	Timestamp    time.Time
	Media        MediaMessage
	MsgType      MessageType
	Message      string
	Unread       bool
	RawMessage   *waE2E.Message
}

type MediaMessage interface {
	whatsmeow.DownloadableMessage
	GetContextInfo() *waE2E.ContextInfo
	GetFileLength() uint64
	GetMimetype() string
}

type MediaMessageWithThumbnail interface {
	MediaMessage
	GetJPEGThumbnail() []byte
}

type MediaMessageWithCaption interface {
	MediaMessage
	GetCaption() string
}

type MediaMessageWithDimensions interface {
	MediaMessage
	GetHeight() uint32
	GetWidth() uint32
}

type MediaMessageWithFileName interface {
	MediaMessage
	GetFileName() string
}

type MediaMessageWithDuration interface {
	MediaMessage
	GetSeconds() uint32
}

func NewPendingMessage(id types.MessageID, chatJID types.JID, senderJID types.JID, pushName string, message string, msgType MessageType, media MediaMessage) MessageModel {
	return MessageModel{
		ID:        id,
		ChatJID:   chatJID,
		SenderJID: senderJID,
		PushName:  pushName,
		Message:   message,
		MsgType:   msgType,
		Media:     media,
		Timestamp: time.Now(),
		IsGroup:   chatJID.Server == types.GroupServer,
		IsFromMe:  true,
	}
}

func (m *MessageModel) TimeSince() string {
	return humanize.Time(m.Timestamp)
}

func (m *MessageModel) MediaSize() *string {
	if m.Media == nil {
		return nil
	}

	bytes := m.Media.GetFileLength()
	mediaSize := humanize.Bytes(bytes)
	return &mediaSize
}

func (model *MessageModel) populateMessage(client *whatsmeow.Client, chatJID types.JID, msg *waE2E.Message) error {
	switch {
	case msg == nil:
		model.MsgType = MessageTypeUnknown
		model.Message = "> Unable to parse message"

	case msg.Conversation != nil, msg.ExtendedTextMessage != nil:
		model.MsgType = MessageTypeText
		model.Message = msg.GetConversation()
		if msg.ExtendedTextMessage != nil {
			extmsg := msg.ExtendedTextMessage
			if extmsg.Text != nil {
				model.Message = *extmsg.Text
			}
		}

	case msg.ReactionMessage != nil:
		model.MsgType = MessageTypeReference
		model.Message = msg.ReactionMessage.GetText()
		model.ReferencesID = msg.ReactionMessage.Key.ID

	case msg.TemplateMessage != nil:
		tplMsg := msg.GetTemplateMessage()
		tpl := tplMsg.GetHydratedTemplate()
		if tpl == nil {
			return fmt.Errorf("Unable to read template message in %s", chatJID)
		}
		model.MsgType = MessageTypeText
		model.Message = tpl.GetHydratedContentText()

	case msg.HighlyStructuredMessage != nil:
		tplMsg := msg.GetHighlyStructuredMessage()
		tpl := tplMsg.GetHydratedHsm()
		if tpl == nil {
			return fmt.Errorf("Unable to read structured template Hsm message in %s", chatJID)
		}
		tpl4r := tpl.GetHydratedTemplate()
		if tpl4r == nil {
			return fmt.Errorf("Unable to read structure template message")
		}
		model.MsgType = MessageTypeText
		model.Message = tpl4r.GetHydratedContentText()

	case msg.CallLogMesssage != nil:
		model.MsgType = MessageTypeText
		outcome := waE2E.CallLogMessage_CallOutcome_name[int32(msg.CallLogMesssage.GetCallOutcome().Number())]
		title := cases.Title(language.English)
		outcome = title.String(outcome)
		durationSeconds := msg.CallLogMesssage.GetDurationSecs()
		duration := time.Duration(durationSeconds) * time.Second
		model.Message = fmt.Sprintf("%s (%s)", outcome, duration.String())

	case msg.TemplateButtonReplyMessage != nil:
		model.MsgType = MessageTypeText
		model.Message = msg.TemplateButtonReplyMessage.GetSelectedDisplayText()
	case msg.ListMessage != nil:
		lstMsg := msg.ListMessage
		model.MsgType = MessageTypeText
		model.Message = lstMsg.GetDescription()
	case msg.ListResponseMessage != nil:
		lstMsg := msg.ListResponseMessage
		model.Message = lstMsg.GetDescription()

	case msg.AlbumMessage != nil:
		model.Message = fmt.Sprintf("Album Message %d images, %d videos", msg.AlbumMessage.GetExpectedImageCount(), msg.AlbumMessage.GetExpectedVideoCount())

	case msg.Chat != nil:
		model.Message = msg.Chat.GetDisplayName()

	case msg.CommentMessage != nil:
		err := model.populateMessage(client, chatJID, msg.CommentMessage.Message)
		if err != nil {
			return err
		}
		target := "> " + msg.CommentMessage.GetTargetMessageKey().GetID()
		model.MsgType = MessageTypeReference
		model.ReferencesID = &target
		model.Message = fmt.Sprintf("%s\n%s", model.Message, target)

	case msg.ContactMessage != nil:
		model.MsgType = MessageTypeReference
		model.Message = msg.ContactMessage.GetDisplayName()
		model.ReferencesID = msg.ContactMessage.Vcard

	case msg.LocationMessage != nil:
		model.MsgType = MessageTypeLocation
		lmsg := msg.LocationMessage
		model.Message = fmt.Sprintf("(%f, %f) %s\n%s\n%s\n%s", lmsg.GetDegreesLatitude(), lmsg.GetDegreesLongitude(), lmsg.GetName(), lmsg.GetAddress(), lmsg.GetURL(), lmsg.GetComment())

	case msg.LiveLocationMessage != nil:
		model.MsgType = MessageTypeLocation
		lmsg := msg.LocationMessage
		model.Message = fmt.Sprintf("(%f, %f) %s\n%s\n%s\n%s", lmsg.GetDegreesLatitude(), lmsg.GetDegreesLongitude(), lmsg.GetName(), lmsg.GetAddress(), lmsg.GetURL(), lmsg.GetComment())

	case msg.AssociatedChildMessage != nil:
		model.populateMessage(client, chatJID, msg.AssociatedChildMessage.Message)
	case msg.GroupMentionedMessage != nil:
		model.populateMessage(client, chatJID, msg.GroupMentionedMessage.Message)
	case msg.EditedMessage != nil:
		model.populateMessage(client, chatJID, msg.EditedMessage.Message)
	case msg.EphemeralMessage != nil:
		model.populateMessage(client, chatJID, msg.EphemeralMessage.Message)

	case msg.ImageMessage != nil:
		model.convertMediaMessage(msg.ImageMessage)
	case msg.AudioMessage != nil:
		model.convertMediaMessage(msg.AudioMessage)
	case msg.StickerMessage != nil:
		model.convertMediaMessage(msg.StickerMessage)
	case msg.VideoMessage != nil:
		model.convertMediaMessage(msg.VideoMessage)
	case msg.PtvMessage != nil:
		model.convertMediaMessage(msg.PtvMessage)
	case msg.DocumentMessage != nil:
		model.convertMediaMessage(msg.DocumentMessage)
	default:
		return fmt.Errorf("Error parsing: %v", msg)
	}

	if model.MsgType == MessageTypeText {
		model.convertRawTextToMarkup(msg)
	}

	return nil
}

func GetMessageModel(client *whatsmeow.Client, chatJID types.JID, msg *db.Message) (*MessageModel, error) {
	if msg.Timestamp.IsZero() {
		return nil, fmt.Errorf("GetMessageModel: timestamp zero")
	}
	model := MessageModel{
		ID:         msg.MessageID,
		ChatJID:    msg.ChatJID,
		SenderJID:  msg.SenderJID,
		PushName:   msg.PushName,
		IsFromMe:   msg.SenderJID == msg.DeviceJID.ToNonAD(),
		IsGroup:    msg.SenderJID == types.GroupServerJID,
		Timestamp:  msg.Timestamp,
		RawMessage: msg.Message,
		MsgType:    MessageTypeUnknown,
	}
	if model.IsFromMe {
		model.PushName = "Me"
	}
	if msg.MessageData != nil {
		if err := msg.LoadMessage(); err != nil {
			return nil, fmt.Errorf("GetMessageModel LoadMessage: %s", err.Error())
		}
		fmt.Printf("LoadMessage from message data %s: %v\n", msg.MessageID, msg.Message)
	}

	emsg := msg.Message
	if err := model.populateMessage(client, chatJID, emsg); err != nil {
		return nil, fmt.Errorf("GetMessageModel populateMessage: %s", err.Error())
	}

	return &model, nil
}

func (mm *MessageModel) convertMediaMessage(rawMsg MediaMessage) {
	mm.Media = rawMsg

	switch rawMsg.(type) {
	case *waE2E.ImageMessage:
		mm.MsgType = MessageTypeImage
	case *waE2E.DocumentMessage:
		mm.MsgType = MessageTypeDocument
	case *waE2E.AudioMessage:
		mm.MsgType = MessageTypeAudio
	case *waE2E.StickerMessage:
		mm.MsgType = MessageTypeImage
	case *waE2E.VideoMessage:
		mm.MsgType = MessageTypeVideo
	}
	if captionMsg, ok := rawMsg.(MediaMessageWithCaption); ok && len(captionMsg.GetCaption()) > 0 {
		mm.Message = captionMsg.GetCaption()
	} else if durationMsg, ok := rawMsg.(MediaMessageWithDuration); ok {
		mm.Message = fmt.Sprintf("Audio Message %d sec", durationMsg.GetSeconds())
	} else if mm.MsgType == MessageTypeUnknown {
		mm.Message = "Unsupported media type"
	}
}

func (mm *MessageModel) convertRawTextToMarkup(msg *waE2E.Message) {
	if msg == nil {
		return
	}
	if len(msg.GetExtendedTextMessage().GetText()) > 0 {
		mm.Message = msg.GetExtendedTextMessage().GetText()
	} else {
		mm.Message = msg.GetConversation()
	}

	//fmt.Printf("Converting raw text to markup: %s", mm.Message)
	// Replace links with html
	mm.Message = urlRegex.ReplaceAllStringFunc(mm.Message, func(match string) string {
		return fmt.Sprintf(`<a href="%s">%s</a>`, match, match)
	})
	//fmt.Printf(" to: %s\n", mm.Message)
}

func (mm *MessageModel) IntoDbMessage(deviceJID *types.JID) db.Message {
	return db.Message{
		DeviceJID: *deviceJID,
		ChatJID:   mm.ChatJID,
		SenderJID: mm.SenderJID,
		MessageID: mm.ID,
		Timestamp: mm.Timestamp,
		Message:   mm.RawMessage,
	}
}
