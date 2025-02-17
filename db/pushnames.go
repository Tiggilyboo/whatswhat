package db

import (
	"context"
	"fmt"

	"go.mau.fi/util/dbutil"
	"go.mau.fi/util/exslices"
	"go.mau.fi/whatsmeow/types"
)

type PushNameQuery struct {
	*dbutil.Database
}

type PushName struct {
	DeviceJID types.JID
	JID       types.JID
	Name      string
}

const (
	upsertHistorySyncPushNameQuery = `
		INSERT INTO whatsapp_history_sync_pushname (
			device_jid, jid, name
		)
		VALUES ($1, $2, $3)
		ON CONFLICT (jid)
		DO UPDATE SET name=whatsapp_history_sync_pushname.name
	`
	getPushNamesQuery = `
		SELECT jid, name FROM whatsapp_history_sync_pushname
		WHERE device_jid=$1
	`

	deletePushNameQuery = `
		DELETE FROM whatsapp_history_sync_pushname
		WHERE device_jid=$1
	`
)

func (pn *PushName) GetMassInsertValues() [2]any {
	return [2]any{pn.JID, pn.Name}
}

var batchInsertPushName = dbutil.NewMassInsertBuilder[*PushName, [1]any](
	upsertHistorySyncPushNameQuery, "($1, $%d, $%d)",
)

func (pq *PushNameQuery) Put(ctx context.Context, deviceJID types.JID, pushNames []*PushName) error {
	return pq.DoTxn(ctx, nil, func(ctx context.Context) error {
		for _, chunk := range exslices.Chunk(pushNames, 50) {
			query, params := batchInsertPushName.Build([1]any{deviceJID}, chunk)
			_, err := pq.Exec(ctx, query, params...)
			if err != nil {
				fmt.Printf("Unable to execute: %s\nErr: %s", query, err.Error())
				return err
			}
		}
		return nil
	})
}

func (pn *PushName) Scan(row dbutil.Scannable) (*PushName, error) {
	err := row.Scan(&pn.DeviceJID, &pn.JID, &pn.Name)
	if err != nil {
		return nil, err
	}
	return pn, nil
}

func (pq PushNameQuery) Get(ctx context.Context, deviceJID types.JID) (map[types.JID]*PushName, error) {
	query := fmt.Sprintf(getPushNamesQuery)
	rows, err := pq.Query(ctx, query, deviceJID)
	if err != nil {
		return nil, fmt.Errorf("Query Error: %s", err.Error())
	}
	defer rows.Close()

	pushNames := make(map[types.JID]*PushName)
	for rows.Next() {
		pushName := PushName{}
		if _, err := pushName.Scan(rows); err != nil {
			return nil, fmt.Errorf("Scan error: %s", err.Error())
		}

		pushNames[pushName.JID] = &pushName
	}

	return pushNames, nil
}

func (pq PushNameQuery) DeleteAll(ctx context.Context) error {
	_, err := pq.Exec(ctx, deletePushNameQuery)
	return err
}
