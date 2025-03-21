package models

type Block struct {
	Number     uint64
	Hash       string
	ParentHash string
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
