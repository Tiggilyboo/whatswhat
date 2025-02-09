package models

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"time"

	"go.mau.fi/util/exmime"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type MessageType uint8

const (
	MessageTypeUnknown MessageType = iota
	MessageTypeText
	MessageTypeImage
	MessageTypeAudio
	MessageTypeVideo
	MessageTypeDocument
)

var inlineURLRegex = regexp.MustCompile(`\[(.+?)]\((.+?)\)`)

type MessageModel struct {
	ID          types.MessageID
	ChatJID     types.JID
	SenderJID   types.JID
	PushName    string
	IsFromMe    bool
	IsGroup     bool
	Timestamp   time.Time
	Media       MediaMessage
	MsgType     MessageType
	Message     string
	FileName    string
	Unread      bool
	ContextInfo *waE2E.ContextInfo
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

func GetMessageModel(client *whatsmeow.Client, chatJID types.JID, msg *events.Message) (*MessageModel, error) {
	info := msg.Info
	if info.Timestamp.IsZero() {
		return nil, fmt.Errorf("GetMessageModel: timestamp zero")
	}
	model := MessageModel{
		ID:        info.ID,
		ChatJID:   info.Chat,
		SenderJID: info.Sender,
		PushName:  info.PushName,
		IsFromMe:  info.IsFromMe,
		IsGroup:   info.IsGroup,
		Timestamp: info.Timestamp,
		MsgType:   MessageTypeUnknown,
	}
	if client.Store.ID.User == info.Sender.User {
		model.PushName = "Me"
	}

	emsg := msg.Message
	if emsg == nil {
		emsg = msg.SourceWebMsg.GetMessage()
	}
	switch {
	case emsg == nil:
		model.MsgType = MessageTypeText
		model.Message = "Unable to parse message"
		fmt.Printf("nil message: %v", msg)

	case emsg.Conversation != nil, msg.Message.ExtendedTextMessage != nil:
		model.MsgType = MessageTypeText
		model.Message = emsg.GetConversation()
		if emsg.ExtendedTextMessage != nil {
			extmsg := emsg.ExtendedTextMessage
			if extmsg.Text != nil {
				model.Message = *extmsg.Text
			}
		}

	case emsg.TemplateMessage != nil:
		tplMsg := emsg.GetTemplateMessage()
		tpl := tplMsg.GetHydratedTemplate()
		if tpl == nil {
			return nil, fmt.Errorf("Unable to read template message in %s", chatJID)
		}
		model.MsgType = MessageTypeText
		model.Message = tpl.GetHydratedContentText()
	case emsg.HighlyStructuredMessage != nil:
		tplMsg := emsg.GetHighlyStructuredMessage()
		tpl := tplMsg.GetHydratedHsm()
		if tpl == nil {
			return nil, fmt.Errorf("Unable to read structured template Hsm message in %s", chatJID)
		}
		tpl4r := tpl.GetHydratedTemplate()
		if tpl4r == nil {
			return nil, fmt.Errorf("Unable to read structure template message")
		}
		model.MsgType = MessageTypeText
		model.Message = tpl4r.GetHydratedContentText()
	case emsg.TemplateButtonReplyMessage != nil:
		model.MsgType = MessageTypeText
		model.Message = emsg.TemplateButtonReplyMessage.GetSelectedDisplayText()
	case emsg.ListMessage != nil:
		lstMsg := emsg.ListMessage
		model.MsgType = MessageTypeText
		model.Message = lstMsg.GetDescription()
	case emsg.ListResponseMessage != nil:
		lstMsg := emsg.ListResponseMessage
		model.Message = lstMsg.GetDescription()
	case emsg.ImageMessage != nil:
		model.convertMediaMessage(emsg.ImageMessage)
	case emsg.AudioMessage != nil:
		model.convertMediaMessage(emsg.AudioMessage)
	case emsg.StickerMessage != nil:
		model.convertMediaMessage(emsg.StickerMessage)
	case emsg.VideoMessage != nil:
		model.convertMediaMessage(emsg.VideoMessage)
	case emsg.PtvMessage != nil:
		model.convertMediaMessage(emsg.PtvMessage)
	case emsg.DocumentMessage != nil:
		model.convertMediaMessage(emsg.DocumentMessage)
	default:
		data, _ := proto.Marshal(emsg)
		encodedMsg := base64.StdEncoding.EncodeToString(data)
		model.Message = fmt.Sprintf("Error parsing: %s", encodedMsg)
	}

	if model.MsgType == MessageTypeText {
		model.convertRawTextToMarkup(emsg)
	}

	return &model, nil
}

func (mm *MessageModel) convertMediaMessage(rawMsg MediaMessage) {
	mm.Media = rawMsg
	mm.ContextInfo = rawMsg.GetContextInfo()

	switch msg := rawMsg.(type) {
	case *waE2E.ImageMessage:
		mm.MsgType = MessageTypeImage
		mm.FileName = "media" + exmime.ExtensionFromMimetype(msg.GetMimetype())
	case *waE2E.DocumentMessage:
		mm.MsgType = MessageTypeDocument
		mm.FileName = "media" + exmime.ExtensionFromMimetype(msg.GetMimetype())
	case *waE2E.AudioMessage:
		mm.MsgType = MessageTypeAudio
		mm.FileName = "media" + exmime.ExtensionFromMimetype(msg.GetMimetype())
	case *waE2E.StickerMessage:
		mm.MsgType = MessageTypeImage
		mm.FileName = "media" + exmime.ExtensionFromMimetype(msg.GetMimetype())
	case *waE2E.VideoMessage:
		mm.MsgType = MessageTypeVideo
		mm.FileName = "media" + exmime.ExtensionFromMimetype(msg.GetMimetype())
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
	mm.ContextInfo = msg.GetExtendedTextMessage().GetContextInfo()

	fmt.Printf("Converting raw text to markup: %s", mm.Message)

	// Replace links with html
	mm.Message = inlineURLRegex.ReplaceAllStringFunc(mm.Message, func(s string) string {
		groups := inlineURLRegex.FindStringSubmatch(s)
		return fmt.Sprintf(`<a href="%s">%s</a>`, groups[2], groups[1])
	})
	fmt.Printf(" to: %s\n", mm.Message)
}
