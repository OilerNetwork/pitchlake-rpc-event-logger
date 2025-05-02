package models

type VaultChannel struct {
	Data *VaultRegistry
	Err  error
}
type StarknetBlocks struct {
	BlockNumber uint64 `json:"block_number"`
	Timestamp   uint64 `json:"timestamp"`
	BlockHash   string `json:"block_hash"`
	ParentHash  string `json:"parent_hash"`
	Status      string `json:"status"`
}

type Event struct {
	From            string   `json:"from"`
	ID              uint     `json:"id"`
	TransactionHash string   `json:"transactionHash"`
	BlockNumber     uint64   `json:"blockNumber"`
	VaultAddress    string   `json:"vaultAddress"`
	Timestamp       uint64   `json:"timestamp"`
	EventName       string   `json:"eventName"`
	EventKeys       []string `json:"eventKeys"`
	EventData       []string `json:"eventData"`
}

type VaultRegistry struct {
	ID                 uint    `json:"id"`
	Address            string  `json:"address"`
	DeployedAt         string  `json:"deployed_at"`
	LastBlockIndexed   *string `json:"last_block_indexed"`
	LastBlockProcessed *string `json:"last_block_processed"`
}
