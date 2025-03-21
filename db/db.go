package db

import (
	"context"
	"log"

	"rpc-indexer/models"

	"github.com/NethermindEth/starknet.go/rpc"
	"github.com/jackc/pgx/v5"
)

type DB struct {
	conn *pgx.Conn
	ctx  context.Context
	tx   pgx.Tx
}

func NewDB(dbPath string, ctx context.Context) (*DB, error) {
	conn, err := pgx.Connect(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close(db.ctx)
}

func (db *DB) BeginTx() error {
	tx, err := db.conn.BeginTx(db.ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	db.tx = tx
	return nil
}

func (db *DB) CommitTx() error {
	return db.tx.Commit(db.ctx)
}

func (db *DB) RollbackTx() error {
	return db.tx.Rollback(db.ctx)
}

func (db *DB) InsertBlock(block *rpc.BlockWithReceipts) error {

	for _, transaction := range block.Transactions {
		log.Printf("Transaction: %v", transaction)
	}
	_, err := db.tx.Exec(
		db.ctx,
		`INSERT INTO "Blocks" 
		(block_number, block_hash, parent_hash) 
		VALUES ($1, $2, $3)`,
		block.BlockNumber,
		block.BlockHash,
		block.ParentHash,
	)
	return err
}

func (db *DB) InsertEvent(event *models.Event) error {
	_, err := db.tx.Exec(
		db.ctx,
		`INSERT INTO "Events" 
		(block_number, vault_address, timestamp, event_name, event_keys, event_data, transaction_hash) 
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.BlockNumber,
		event.VaultAddress,
		event.Timestamp,
		event.EventName,
		event.EventKeys, // pgx/v5 automatically handles []string to PostgreSQL string array
		event.EventData, // pgx/v5 automatically handles []string to PostgreSQL string array
		event.TransactionHash,
	)
	return err
}

func (db *DB) GetBlockByNumber(blockNumber uint64) {

}
