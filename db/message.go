package db

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/util/dbutil"
	"go.mau.fi/util/exslices"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type MessageQuery struct {
	*dbutil.Database
}

const (
	insertMessageQuery = `
		INSERT INTO whatsapp_history_sync_message (device_jid, chat_jid, sender_jid, message_id, timestamp, push_name, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (device_jid, chat_jid, sender_jid, message_id) DO NOTHING
	`
	getMessagesBetweenQueryTemplate = `
		SELECT message_id, sender_jid, timestamp, push_name, data FROM whatsapp_history_sync_message
		WHERE device_jid=$1 AND chat_jid=$2
			%s
		ORDER BY timestamp DESC
		%s
	`
	deleteMessagesBetweenQuery = `
		DELETE FROM whatsapp_history_sync_message
		WHERE device_jid=$1 AND chat_jid=$2 AND timestamp<=$3 AND timestamp>=$4
	`
	deleteAllMessagesQuery       = "DELETE FROM whatsapp_history_sync_message WHERE device_jid=$1"
	deleteMessagesForPortalQuery = `
		DELETE FROM whatsapp_history_sync_message
		WHERE device_jid=$1 AND chat_jid=$2
	`
	conversationHasMessagesQuery = `
		SELECT EXISTS(
		    SELECT 1 FROM whatsapp_history_sync_message
			WHERE device_jid=$1 AND chat_jid=$2
		)
	`
)

type Message struct {
	DeviceJID   types.JID
	ChatJID     types.JID
	SenderJID   types.JID
	MessageID   types.MessageID
	Timestamp   time.Time
	PushName    string
	MessageData []byte
	Message     *waE2E.Message
}

func NewMessageFromEvent(deviceJID types.JID, evt *events.Message) (*Message, error) {
	if evt.Message == nil {
		return nil, fmt.Errorf("Event message is nil")
	}
	return &Message{
		DeviceJID: deviceJID,
		ChatJID:   evt.Info.Chat,
		SenderJID: evt.Info.Sender.ToNonAD(),
		MessageID: evt.Info.ID,
		PushName:  evt.Info.PushName,
		Timestamp: evt.Info.Timestamp,
		Message:   evt.Message,
	}, nil
}

func (m *Message) GetMassInsertValues() [5]any {
	if m.MessageData == nil && m.Message != nil {
		err := m.SaveMessageData()
		if err != nil {
			fmt.Printf("Error saving message %s data: %v", m.MessageID, m.MessageData)
		}
	}
	return [5]any{m.SenderJID.ToNonAD(), m.MessageID, m.Timestamp.Unix(), m.PushName, m.MessageData}
}

var batchInsertMessage = dbutil.NewMassInsertBuilder[*Message, [2]any](
	insertMessageQuery, "($1, $2, $%d, $%d, $%d, $%d, $%d)",
)

func (m *Message) LoadMessage() error {
	return proto.Unmarshal(m.MessageData, m.Message)
}

func (m *Message) SaveMessageData() error {
	bytes, err := proto.Marshal(m.Message)
	if err != nil {
		return err
	}
	m.MessageData = bytes
	return nil
}

func (mq *MessageQuery) Put(ctx context.Context, deviceJID types.JID, chatJID types.JID, messages []*Message) error {
	return mq.DoTxn(ctx, nil, func(ctx context.Context) error {
		for _, chunk := range exslices.Chunk(messages, 50) {
			query, params := batchInsertMessage.Build([2]any{deviceJID, chatJID}, chunk)
			_, err := mq.Exec(ctx, query, params...)
			if err != nil {
				fmt.Printf("Unable to execute: %s\nErr: %s", query, err.Error())
				return err
			}
		}
		return nil
	})
}

func (m *Message) Scan(row dbutil.Scannable) (*Message, error) {
	var timestampUnix int64
	err := row.Scan(&m.MessageID, &m.SenderJID, &timestampUnix, &m.PushName, &m.MessageData)
	if err != nil {
		return nil, fmt.Errorf("Unable to scan row: %s", err.Error())
	}
	m.Timestamp = time.Unix(timestampUnix, 0)
	if m.Timestamp.IsZero() {
		return nil, fmt.Errorf("Zero timestamp: %s", m.MessageID)
	}
	if m.MessageData == nil {
		return nil, fmt.Errorf("Unable to load message data: %s", m.MessageID)
	}

	return m, nil
}

func (mq *MessageQuery) GetBetween(ctx context.Context, deviceJID types.JID, chatJID types.JID, startTime, endTime *time.Time, limit int) ([]Message, error) {
	whereClauses := ""
	args := []any{deviceJID, chatJID}
	argNum := 3
	if startTime != nil {
		whereClauses += fmt.Sprintf(" AND timestamp >= $%d", argNum)
		args = append(args, startTime.Unix())
		argNum++
	}
	if endTime != nil {
		whereClauses += fmt.Sprintf(" AND timestamp <= $%d", argNum)
		args = append(args, endTime.Unix())
		argNum++
	}

	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", limit)
	}
	query := fmt.Sprintf(getMessagesBetweenQueryTemplate, whereClauses, limitClause)

	//fmt.Printf("Query: %s\n between %s and %s", query, startTime, endTime)

	rows, err := mq.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("Query Error: %s", err.Error())
	}
	defer rows.Close()

	more := true
	count := 0
	messages := make([]Message, limit)
	for rows.Next() {
		if !more {
			break
		}
		message := Message{}
		message.DeviceJID = deviceJID
		message.ChatJID = chatJID
		if _, err := message.Scan(rows); err != nil {
			return nil, fmt.Errorf("Scan error: %s", err.Error())
		}

		count++
	}
	messages = messages[:count]

	return messages, nil
}

func (mq *MessageQuery) DeleteBetween(ctx context.Context, deviceJID types.JID, chatJID types.JID, before, after uint64) error {
	_, err := mq.Exec(ctx, deleteMessagesBetweenQuery, deviceJID, chatJID, before, after)
	if err != nil {
		fmt.Printf("Unable to execute: %s\n with params: %v\n", deleteMessagesBetweenQuery, []any{deviceJID, chatJID, before, after})
	}
	return err
}

func (mq *MessageQuery) DeleteAll(ctx context.Context, deviceJID types.JID) error {
	_, err := mq.Exec(ctx, deleteAllMessagesQuery, deviceJID)
	if err != nil {
		fmt.Printf("Unable to execute: %s\n with params: %v\n", deleteAllMessagesQuery, deviceJID)
	}
	return err
}

func (mq *MessageQuery) DeleteAllInChat(ctx context.Context, deviceJID types.JID, chatJID types.JID) error {
	_, err := mq.Exec(ctx, deleteMessagesForPortalQuery, deviceJID, chatJID)
	if err != nil {
		fmt.Printf("Unable to execute: %s\n with params: %v\n", deleteMessagesForPortalQuery, []any{deviceJID, chatJID})
	}
	return err
}

func (mq *MessageQuery) ConversationHasMessages(ctx context.Context, deviceJID types.JID, chatJID types.JID) (exists bool, err error) {
	err = mq.QueryRow(ctx, conversationHasMessagesQuery, deviceJID, chatJID).Scan(&exists)
	if err != nil {
		fmt.Printf("Unable to execute: %s\n with params: %v\n", conversationHasMessagesQuery, []any{deviceJID, chatJID})
	}
	return
}
