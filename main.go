package main

import (
	"context"
	"log"
	"os"
	"rpc-indexer/db"
	"rpc-indexer/models"
	"rpc-indexer/network"
	"rpc-indexer/utils"
	"strconv"
	"sync"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/rpc"
)

type Indexer struct {
	network           *network.Network
	db                *db.DB
	head              *models.StarknetBlocks
	lastBlockDB       *models.StarknetBlocks
	mu                sync.Mutex
	channel           chan models.VaultChannel
	cursor            uint64
	vaultAddressesMap map[string]struct{}
	udcAddress        string
	log               *log.Logger
}

func (i *Indexer) Init() error {
	dbUrl := os.Getenv("DB_URL")
	udcAddress := os.Getenv("UDC_ADDRESS")
	cursor := os.Getenv("CURSOR")
	rpcUrl := os.Getenv("RPC_URL")
	var err error
	dbClient, err := db.NewDB(dbUrl, context.Background())
	if err != nil {
		return err
	}
	i.db = dbClient

	i.network = network.NewNetwork(rpcUrl)
	if err != nil {
		return err
	}
	if cursor != "" {
		i.cursor, err = strconv.ParseUint(cursor, 10, 64)
		if err != nil {
			return err
		}
	}
	i.lastBlockDB, err = i.db.GetLastBlock()
	if err != nil {
		return err
	}
	i.udcAddress = udcAddress
	i.vaultAddressesMap = make(map[string]struct{})

	vaultRegistry, err := i.db.GetVaultRegistry()
	if err != nil {
		return err
	}

	//Map
	if len(vaultRegistry) > 0 {
		for _, vault := range vaultRegistry {
			if vault.LastBlockIndexed != &i.lastBlockDB.BlockHash {
				i.CatchupVault(vault.Address, i.lastBlockDB.BlockNumber)
				i.vaultAddressesMap[vault.Address] = struct{}{}
			}
		}
	}

	i.log = log.Default()
	i.channel = make(chan models.VaultChannel)
	if err != nil {
		return err
	}
	log.Printf("Vault addresses: %v", i.vaultAddressesMap)
	log.Printf("Last block: %v", i.lastBlockDB)
	log.Printf("Cursor: %v", i.cursor)
	err = i.Listener()
	if err != nil {
		return err
	}
	return nil
}

func (i *Indexer) Listener() error {
	i.db.ListenerNewVault(i.channel)
	for {
		vault := <-i.channel
		if vault.Err != nil {
			log.Printf("Error: %v", vault.Err)
			return vault.Err
		}
		i.InitializeVault(*vault.Data)
	}
	// Unreachable code as the loop is infinite
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

	for {
		select {
		case newHead := <-newHeadsChan:
			indexer.mu.Lock()
			log.Printf("New block head: %v", newHead)

			block, err := indexer.network.GetBlock(newHead)
			if err != nil {
				log.Fatalf("Failed to get block: %v", err)
			}
			lastBlock, err := indexer.db.GetLastBlock()
			if err != nil {
				log.Fatalf("Failed to get last block: %v", err)
			}
			if lastBlock.BlockHash != block.ParentHash.String() {
				log.Printf("Last block hash does not match parent hash: %v", lastBlock.BlockHash)
				newHeadBlock, err := indexer.handleReorg(&block.BlockHeader)
				if err != nil {
					log.Fatalf("Failed to handle reorg: %v", err)
				}
				indexer.lastBlockDB = newHeadBlock
				indexer.CatchupIndexer(block.Number - 1)
			}

			for _, tx := range block.Transactions {
				for _, event := range tx.Receipt.Events {
					log.Printf("Event: %v", event)
					fromAddress := event.FromAddress.String()
					if _, exists := indexer.vaultAddressesMap[fromAddress]; exists {
						err := indexer.processVaultEvent(tx.Transaction.Hash().String(), fromAddress, &event, block.Number)
						if err != nil {
							indexer.db.RollbackTx()
							indexer.log.Println("Error processing vault event", err)
							log.Fatal("Error processing vault event", err)
							//Handle error to retry without shutdown
						}
					}
				}
			}
			newBlock := models.StarknetBlocks{
				BlockNumber: block.Number,
				Timestamp:   block.Timestamp,
				BlockHash:   block.Hash.String(),
				ParentHash:  block.ParentHash.String(),
				Status:      "MINED",
			}
			err = indexer.db.InsertBlock(newBlock)
			if err != nil {
				log.Fatalf("Failed to insert block: %v", err)
				//Handle error to retry without shutdown
			}
			indexer.lastBlockDB = &newBlock
			indexer.mu.Unlock()
		case err := <-indexer.network.Sub.Err():
			if err != nil {
				log.Fatalf("Subscription error: %v", err)
				//Handle error to retry without shutdown
			}
		}
	}

	//Add script to fetch initial block here later

}

func (i *Indexer) CatchupIndexer(latestBlock uint64) error {

	startBlock := i.lastBlockDB.BlockNumber
	for startBlock < latestBlock {
		endBlock := startBlock + 1000
		endBlock = min(endBlock, latestBlock)
		for vault := range i.vaultAddressesMap {
			err := i.CatchupVault(vault, endBlock)
			if err != nil {
				log.Println("Error catching up vault", err)
				return err
			}
		}
		log.Println("Catching up indexer from", startBlock, "to", endBlock)
		blocks, err := i.network.GetBlocks(startBlock, endBlock)
		if err != nil {
			log.Println("Error getting blocks", err)
			return err
		}
		for _, block := range blocks {
			err := i.db.InsertBlock(*block)
			if err != nil {
				log.Println("Error inserting block", err)
				return err
			}
		}
		startBlock = endBlock
	}

	return nil
}

func (i *Indexer) handleReorg(block *rpc.BlockHeader) (*models.StarknetBlocks, error) {

	var returnBlock *models.StarknetBlocks
	queryBlock := rpc.BlockHeader{
		Hash: block.ParentHash,
	}
	parentBlock, err := i.network.GetBlock(&queryBlock)
	if err != nil {
		log.Fatalf("Failed to get block: %v", err)
	}
	parentBlockDB, err := i.db.GetBlockByNumber(parentBlock.Number)
	if err != nil {
		log.Fatalf("Failed to get block: %v", err)
	}
	err = i.db.RevertBlock(parentBlockDB.BlockNumber, parentBlockDB.BlockHash)
	if err != nil {
		log.Fatalf("Failed to revert block: %v", err)
	}
	var parentHashFelt felt.Felt
	parentHashFelt.SetString(parentBlockDB.ParentHash)
	if err != nil {
		log.Fatalf("Failed to set string: %v", err)
	}
	if parentHashFelt != *parentBlock.ParentHash {
		returnBlock, err = i.handleReorg(&parentBlock.BlockHeader)
		if err != nil {
			log.Fatalf("Failed to handle reorg: %v", err)
		}
	}
	if returnBlock != nil {
		return returnBlock, nil
	}
	return parentBlockDB, nil
}

func (i *Indexer) InitializeVault(vault models.VaultRegistry) error {

	deployBlockHash, err := utils.HexStringToFelt(vault.DeployedAt)
	if err != nil {
		log.Println("Error getting felt", err)
		return err
	}
	hash := felt.FromBytes(deployBlockHash)

	hash.SetString(vault.DeployedAt)
	deployBlock := rpc.BlockID{
		Hash: &hash,
	}
	log.Printf("Deploy block: %v", deployBlock)
	events, err := i.network.GetEvents(deployBlock, deployBlock, nil)
	if err != nil {
		log.Println("Error getting events", err)
		return err
	}
	i.db.BeginTx()
	err = i.processDeploymentBlockEvents(events, vault)
	if err != nil {
		log.Println("Error processing deployment events", err)
		i.db.RollbackTx()
		return err
	}
	i.db.CommitTx()
	log.Printf("Processing catchup vault")
	err = i.CatchupVault(vault.Address, i.lastBlockDB.BlockNumber)
	if err != nil {
		return err
	}
	i.vaultAddressesMap[vault.Address] = struct{}{}
	return nil
}

func (i *Indexer) CatchupVault(address string, toBlock uint64) error {
	vaultRegistry, err := i.db.GetVaultRegistryByAddress(address)
	if err != nil {
		log.Println("Error getting vault registry", err)
		return err
	}
	log.Printf("Vault registry: %v", vaultRegistry)

	var fromBlock *rpc.BlockID
	log.Printf("Vault registry: %v", vaultRegistry.LastBlockIndexed)
	if vaultRegistry.LastBlockIndexed == nil {
		err = i.InitializeVault(vaultRegistry)
		if err != nil {
			log.Println("Error initializing vault", err)
			return err
		}
		deployBlock, err := i.network.GetBlockByHash(vaultRegistry.DeployedAt)
		if err != nil {
			log.Println("Error getting deploy block", err)
			return err
		}
		nextBlockNumber := deployBlock.Number + 1
		fromBlock = &rpc.BlockID{Number: &nextBlockNumber}
	} else {
		log.Printf("Last block indexed: %v", vaultRegistry)
		hash := *vaultRegistry.LastBlockIndexed
		nextBlock, err := i.db.GetNextBlock(hash)
		if err != nil {
			log.Println("Block not found, wait to catch up", err)
			return nil
		}
		fromBlock = &rpc.BlockID{Number: &nextBlock.BlockNumber}
	}
	log.Printf("From block: %v", fromBlock)

	if *fromBlock.Number > toBlock {
		log.Println("From block is greater than to block, wait to catch up")
		return nil
	}
	events, err := i.network.GetEvents(*fromBlock, rpc.BlockID{Number: &toBlock}, &address)
	if err != nil {
		log.Println("Error getting events", err)
		return err
	}
	for _, event := range events.Events {
		err := i.processVaultEvent(event.TransactionHash.String(), address, &event.Event, event.BlockNumber)
		if err != nil {
			log.Println("Error processing vault event", err)
			return err
		}
	}
	return nil
}

func (i *Indexer) processVaultEvent(
	txHash string,
	vaultAddress string,
	event *rpc.Event,
	blockNumber uint64,
) error {

	// Store the event in the database
	normalizedVaultAddress, err := utils.NormalizeHexAddress(vaultAddress)
	if err != nil {
		log.Printf("Error normalizing address %v", err)
		return err
	}
	eventName, err := utils.DecodeEventNameVault(event.Keys[0].String())
	if err != nil {
		log.Printf("Unknown Event")
		return nil
	}

	// Store the event in the database
	eventKeys, eventData := utils.EventToStringArrays(*event)
	if err := i.db.StoreEvent(txHash, normalizedVaultAddress, blockNumber, eventName, eventKeys, eventData); err != nil {
		return err
	}
	return nil
}

func (i *Indexer) processDeploymentBlockEvents(
	events *rpc.EventChunk,
	vault models.VaultRegistry,
) error {

	eventNameHash := utils.Keccak256("ContractDeployed")
	for index, event := range events.Events {

		log.Printf("index: %v", index)
		log.Printf("Event from address: %v", event.FromAddress.String())
		log.Printf("UDC address: %v", i.udcAddress)
		if eventNameHash == event.Keys[0].String() && event.FromAddress.String() == i.udcAddress {
			log.Printf("Match")
			address := utils.FeltToHexString(event.Data[0].Bytes())
			log.Printf("Address: %v", address)
			log.Printf("Vault address: %v", vault.Address)
			normalizedVaultAddress, err := utils.NormalizeHexAddress(vault.Address)
			if err != nil {
				log.Printf("Error normalizing address", err)
				return err
			}
			if address == normalizedVaultAddress {
				txHash := utils.FeltToHexString(event.TransactionHash.Bytes())

				eventKeys := utils.FeltArrayToStringArrays(event.Keys)
				eventData := utils.FeltArrayToStringArrays(event.Data)

				i.db.StoreEvent(txHash, address, event.BlockNumber, "ContractDeployed", eventKeys, eventData)

				break

			}

		}
	}

	//Process other vault events in this block
	for _, event := range events.Events {
		normalizedVaultAddress, err := utils.NormalizeHexAddress(vault.Address)
		if err != nil {
			log.Printf("Error normalizing address %v", err)
			return err
		}
		if utils.FeltToHexString(event.FromAddress.Bytes()) == normalizedVaultAddress {
			err := i.processVaultEvent(event.TransactionHash.String(), vault.Address, &event.Event, event.BlockNumber)
			if err != nil {
				return err
			}
			i.db.UpdateVaultRegistry(vault.Address, event.BlockHash.String())
		}
	}
	return nil
}
