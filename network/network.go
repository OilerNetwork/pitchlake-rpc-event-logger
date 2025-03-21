package network

import (
	"context"
	"log"

	"github.com/NethermindEth/starknet.go/client"
	"github.com/NethermindEth/starknet.go/rpc"
)

type Network struct {
	client   *rpc.Provider
	url      string
	wsClient *rpc.WsProvider
	Sub      *client.ClientSubscription
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
	}
}

func (rpcObject *Network) WatchBlockHeads(channel chan *rpc.BlockHeader) error {
	sub, err := rpcObject.wsClient.SubscribeNewHeads(context.Background(), channel, nil)
	if err != nil {
		return err
	}
	rpcObject.Sub = sub
	return nil
}
func (rpcObject *Network) GetBlock(head *rpc.BlockHeader) (*rpc.BlockWithReceipts, error) {
	blockID := &rpc.BlockID{
		Hash: head.BlockHash,
	}
	block, err := rpcObject.client.BlockWithReceipts(context.Background(), *blockID)
	if err != nil {
		return nil, err
	}
	return block.(*rpc.BlockWithReceipts), nil
}
