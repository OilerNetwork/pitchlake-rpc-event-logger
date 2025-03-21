package main

import (
	"context"
	"log"
	"os"
	"rpc-indexer/db"
	"rpc-indexer/network"

	"github.com/NethermindEth/starknet.go/rpc"
)

type Indexer struct {
	network *network.Network
	db      *db.DB
}

func main() {
	//Initialize starknet rpc client
	rpcUrl := os.Getenv("RPC_URL")
	dbUrl := os.Getenv("DB_URL")
	ctx := context.Background()
	db, err := db.NewDB(dbUrl, ctx)
	if err != nil {
		log.Fatalf("Failed to create db: %v", err)
	}
	indexer := Indexer{
		network: network.NewNetwork(rpcUrl),
		db:      db,
	}

	newHeadsChan := make(chan *rpc.BlockHeader)
	err = indexer.network.WatchBlockHeads(newHeadsChan)
	if err != nil {
		log.Fatalf("Failed to watch block heads: %v", err)
	}

loop:
	for {
		select {
		case newHead := <-newHeadsChan:
			log.Printf("New block head: %v", newHead)
			block, err := indexer.network.GetBlock(newHead)
			if err != nil {
				log.Fatalf("Failed to get block: %v", err)
			}
			indexer.db.InsertBlock(block)
		case err := <-indexer.network.Sub.Err():
			if err != nil {
				log.Fatalf("Subscription error: %v", err)
			}
			break loop
		}
	}

	//Add script to fetch initial block here later

}
