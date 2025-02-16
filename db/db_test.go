package db_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/tiggilyboo/whatswhat/db"
	"go.mau.fi/util/dbutil"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	wlog "go.mau.fi/whatsmeow/util/log"
)

func GivenAChatDb(t *testing.T) *db.Database {
	err := os.Remove("test.db")
	if err != nil && !os.IsNotExist(err) {
		t.Error(err)
	}

	sqlDb, err := sql.Open("sqlite3", "file:test.db?_foreign_keys=on")
	if err != nil {
		t.Error(err)
	}
	dbLog := wlog.Stdout("TestDatabase", "DEBUG", true)
	container := sqlstore.NewWithDB(sqlDb, "sqlite3", dbLog)
	if err := container.Upgrade(); err != nil {
		t.Error(err)
	}

	wrappedDb, err := dbutil.NewWithDB(sqlDb, "sqlite3")
	if err != nil {
		t.Error(err)
	}

	chatDbLog := zerolog.New(os.Stdout)
	chatDb := db.New(wrappedDb, chatDbLog)

	upgradeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := chatDb.Upgrade(upgradeCtx); err != nil {
		t.Error(err)
	}

	return chatDb
}

func ConversationQueryPut(t *testing.T) {
	chatDb := GivenAChatDb(t)

	testCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	testJID := types.NewJID("TestUser", "TestServer")
	testChatJID := types.NewJID("TestChat", "TestServer")

	testDbConvoID := "TestingId"
	testDbMessages := make([]*waHistorySync.HistorySyncMsg, 5)
	for i := range 5 {
		testMsgID := uint64(i)
		testMessage := waE2E.Message{
			Conversation: &testDbConvoID,
		}
		testMessageInfo := waWeb.WebMessageInfo{
			Message: &testMessage,
		}
		testDbMessage := waHistorySync.HistorySyncMsg{
			MsgOrderID: &testMsgID,
			Message:    &testMessageInfo,
		}
		testDbMessages[i] = &testDbMessage
	}

	testDbConvo := waHistorySync.Conversation{
		ID:       &testDbConvoID,
		NewJID:   nil,
		OldJID:   nil,
		Messages: testDbMessages,
	}
	testConvo := db.NewConversation(testJID, testChatJID, &testDbConvo)

	err := chatDb.Conversation.Put(testCtx, testJID, testConvo)
	if err != nil {
		t.Error(err)
	}

	tuple := make([]*db.Message, len(testDbMessages))
	for i, _ := range testDbConvo.GetMessages() {
		tupleItem := db.Message{
			MessageID:   "FakeMessageID",
			MessageData: make([]byte, 122),
		}
		tuple[i] = &tupleItem
	}

	err = chatDb.Message.Put(testCtx, testJID, testChatJID, tuple)
	if err != nil {
		t.Error(err)
	}

	if testCtx.Err() == context.Canceled {
		t.Error(testCtx.Err())
	}
}
