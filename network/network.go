package network

import (
	"context"
	"fmt"
	"log"
	"rpc-indexer/models"
	"rpc-indexer/utils"
	"sync"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/client"
	"github.com/NethermindEth/starknet.go/rpc"
)

type Network struct {
	client   *rpc.Provider
	url      string
	wsClient *rpc.WsProvider
	Sub      *client.ClientSubscription
	ctx      context.Context
}

func NewNetwork(url string) *Network {
	wsClient, err := rpc.NewWebsocketProvider(url)
	if err != nil {
		log.Fatalf("Failed to create RPC client: %v", err)
	}
	client, err := rpc.NewProvider(url)
	if err != nil {
		log.Fatalf("Failed to create RPC client: %v", err)
	}

	return &Network{
		client:   client,
		url:      url,
		wsClient: wsClient,
		ctx:      context.TODO(),
	}
}

func (n *Network) GetBlockByHash(hash string) (*rpc.BlockTxHashes, error) {
	feltString, err := utils.HexStringToFelt(hash)
	if err != nil {
		return nil, err
	}
	hashFelt := felt.FromBytes(feltString)
	block, err := n.client.BlockWithTxHashes(n.ctx, rpc.BlockID{Hash: &hashFelt})
	if err != nil {
		return nil, err
	}
	blockTxHashes, ok := block.(*rpc.BlockTxHashes)
	if !ok {
		return nil, fmt.Errorf("unexpected block type for block %v", hash)
	}
	return blockTxHashes, nil
}

func (n *Network) GetEvents(fromBlock rpc.BlockID, toBlock rpc.BlockID, address *string) (*rpc.EventChunk, error) {
	var addressFelt felt.Felt
	filter := rpc.EventFilter{
		FromBlock: fromBlock,
		ToBlock:   toBlock,
	}
	if address != nil {
		addreshHash, err := utils.HexStringToFelt(*address)
		if err != nil {
			log.Printf("Error getting felt", err)
			return nil, err
		}
		addressFelt.SetBytes(addreshHash)
		filter.Address = &addressFelt
	}

	log.Printf("Filter: %v", filter)
	input := rpc.EventsInput{
		EventFilter: filter,
		ResultPageRequest: rpc.ResultPageRequest{
			ChunkSize: 10,
		},
	}
	events, err := n.client.Events(n.ctx, input)
	if err != nil {
		log.Printf("Error getting events", err)
		return nil, err
	}
	return events, nil
}

func (rpcObject *Network) WatchBlockHeads(channel chan *rpc.BlockHeader) error {
	sub, err := rpcObject.wsClient.SubscribeNewHeads(context.Background(), channel, rpc.BlockID{})
	if err != nil {
		return err
	}
	rpcObject.Sub = sub
	return nil
}
func (rpcObject *Network) GetBlock(head *rpc.BlockHeader) (*rpc.BlockWithReceipts, error) {
	blockID := &rpc.BlockID{
		Hash: head.Hash,
	}
	block, err := rpcObject.client.BlockWithReceipts(context.Background(), *blockID)
	if err != nil {
		return nil, err
	}
	return block.(*rpc.BlockWithReceipts), nil
}

func (n *Network) GetBlocks(fromBlock uint64, toBlock uint64) ([]*models.StarknetBlocks, error) {
	numBlocks := toBlock - fromBlock + 1
	blocks := make([]*models.StarknetBlocks, numBlocks)

	// Use a WaitGroup to wait for all goroutines to finish
	var wg sync.WaitGroup
	// Channel for errors
	errCh := make(chan error, numBlocks)
	// Semaphore to limit concurrency
	maxConcurrent := 100 // Adjust based on your needs
	sem := make(chan struct{}, maxConcurrent)

	for i := fromBlock; i <= toBlock; i++ {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(blockNum uint64, index uint64) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			log.Printf("Getting block %v", blockNum)
			block, err := n.client.BlockWithTxHashes(n.ctx, rpc.BlockID{Number: &blockNum})
			if err != nil {
				log.Printf("Error getting block %v: %v", blockNum, err)
				errCh <- err
				return
			}

			blockTxHashes, ok := block.(*rpc.BlockTxHashes)
			if !ok {
				errCh <- fmt.Errorf("unexpected block type for block %v", blockNum)
				return
			}

			blockHeader := blockTxHashes.BlockHeader
			starknetBlock := &models.StarknetBlocks{
				BlockNumber: blockNum,
				BlockHash:   blockHeader.Hash.String(),
				ParentHash:  blockHeader.ParentHash.String(),
				Timestamp:   blockHeader.Timestamp,
			}

			// Store in the correct position in the result slice
			blocks[index-fromBlock] = starknetBlock
			log.Printf("Processed block %v", blockNum)
		}(i, i)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errCh)

	// Check for any errors
	select {
	case err := <-errCh:
		return nil, err
	default:
		return blocks, nil
	}
}
