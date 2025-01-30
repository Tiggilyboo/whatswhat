package db

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/util/dbutil"
	"go.mau.fi/util/exslices"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type MessageQuery struct {
	*dbutil.Database
}

const (
	insertHistorySyncMessageQuery = `
		INSERT INTO whatsapp_history_sync_message (device_jid, chat_jid, sender_jid, message_id, timestamp, data, inserted_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (device_jid, chat_jid, sender_jid, message_id) DO NOTHING
	`
	getHistorySyncMessagesBetweenQueryTemplate = `
		SELECT data FROM whatsapp_history_sync_message
		WHERE device_jid=$1 AND chat_jid=$2
			%s
		ORDER BY timestamp DESC
		%s
	`
	deleteHistorySyncMessagesBetweenQuery = `
		DELETE FROM whatsapp_history_sync_message
		WHERE device_jid=$1 AND chat_jid=$2 AND timestamp<=$3 AND timestamp>=$4
	`
	deleteAllHistorySyncMessagesQuery       = "DELETE FROM whatsapp_history_sync_message WHERE device_jid=$1"
	deleteHistorySyncMessagesForPortalQuery = `
		DELETE FROM whatsapp_history_sync_message
		WHERE device_jid=$1 AND chat_jid=$2
	`
	conversationHasHistorySyncMessagesQuery = `
		SELECT EXISTS(
		    SELECT 1 FROM whatsapp_history_sync_message
			WHERE device_jid=$1 AND chat_jid=$2
		)
	`
)

type HistorySyncMessageTuple struct {
	Info    *types.MessageInfo
	Message []byte
}

func (t *HistorySyncMessageTuple) GetMassInsertValues() [4]any {
	return [4]any{t.Info.Sender.ToNonAD(), t.Info.ID, t.Info.Timestamp.Unix(), t.Message}
}

var batchInsertHistorySyncMessage = dbutil.NewMassInsertBuilder[*HistorySyncMessageTuple, [3]any](
	insertHistorySyncMessageQuery, "($1, $2, $%d, $%d, $%d, $%d, $3)",
)

func (mq *MessageQuery) Put(ctx context.Context, deviceJID types.JID, chatJID types.JID, messages []*HistorySyncMessageTuple) error {
	return mq.DoTxn(ctx, nil, func(ctx context.Context) error {
		for _, chunk := range exslices.Chunk(messages, 50) {
			query, params := batchInsertHistorySyncMessage.Build([3]any{deviceJID, chatJID, time.Now().Unix()}, chunk)
			_, err := mq.Exec(ctx, query, params...)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func scanWebMessageInfo(rows dbutil.Scannable) (*waWeb.WebMessageInfo, error) {
	var msgData []byte
	err := rows.Scan(&msgData)
	if err != nil {
		return nil, err
	}
	var historySyncMsg waHistorySync.HistorySyncMsg
	err = proto.Unmarshal(msgData, &historySyncMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}
	return historySyncMsg.GetMessage(), nil
}

var webMessageInfoConverter = dbutil.ConvertRowFn[*waWeb.WebMessageInfo](scanWebMessageInfo)

func (mq *MessageQuery) GetBetween(ctx context.Context, deviceJID types.JID, chatJID types.JID, startTime, endTime *time.Time, limit int) ([]*waWeb.WebMessageInfo, error) {
	whereClauses := ""
	args := []any{deviceJID, chatJID}
	argNum := 4
	if startTime != nil {
		whereClauses += fmt.Sprintf(" AND timestamp >= $%d", argNum)
		args = append(args, startTime.Unix())
		argNum++
	}
	if endTime != nil {
		whereClauses += fmt.Sprintf(" AND timestamp <= $%d", argNum)
		args = append(args, endTime.Unix())
	}

	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", limit)
	}
	query := fmt.Sprintf(getHistorySyncMessagesBetweenQueryTemplate, whereClauses, limitClause)

	return webMessageInfoConverter.
		NewRowIter(mq.Query(ctx, query, args...)).
		AsList()
}

func (mq *MessageQuery) DeleteBetween(ctx context.Context, deviceJID types.JID, chatJID types.JID, before, after uint64) error {
	_, err := mq.Exec(ctx, deleteHistorySyncMessagesBetweenQuery, deviceJID, chatJID, before, after)
	return err
}

func (mq *MessageQuery) DeleteAll(ctx context.Context, deviceJID types.JID) error {
	_, err := mq.Exec(ctx, deleteAllHistorySyncMessagesQuery, deviceJID)
	return err
}

func (mq *MessageQuery) DeleteAllInChat(ctx context.Context, deviceJID types.JID, chatJID types.JID) error {
	_, err := mq.Exec(ctx, deleteHistorySyncMessagesForPortalQuery, deviceJID, chatJID)
	return err
}

func (mq *MessageQuery) ConversationHasMessages(ctx context.Context, deviceJID types.JID, chatJID types.JID) (exists bool, err error) {
	err = mq.QueryRow(ctx, conversationHasHistorySyncMessagesQuery, deviceJID, chatJID).Scan(&exists)
	return
}
