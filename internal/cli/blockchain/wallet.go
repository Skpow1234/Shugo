package blockchain

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Skpow1234/Shugo/internal/cli/client"
	shugocrypto "github.com/Skpow1234/Shugo/internal/crypto"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
)

// Wallet represents a blockchain wallet
type Wallet struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Address    string                 `json:"address"`
	PrivateKey string                 `json:"private_key,omitempty"` // Encrypted in real implementation
	PublicKey  string                 `json:"public_key"`
	Balance    string                 `json:"balance"`
	Currency   string                 `json:"currency"`
	Network    string                 `json:"network"`
	CreatedAt  time.Time              `json:"created_at"`
	LastUsed   time.Time              `json:"last_used"`
	IsActive   bool                   `json:"is_active"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// Transaction represents a blockchain transaction
type Transaction struct {
	ID          string                 `json:"id"`
	From        string                 `json:"from"`
	To          string                 `json:"to"`
	Amount      string                 `json:"amount"`
	Currency    string                 `json:"currency"`
	GasPrice    string                 `json:"gas_price"`
	GasLimit    string                 `json:"gas_limit"`
	Nonce       uint64                 `json:"nonce"`
	Hash        string                 `json:"hash"`
	Status      string                 `json:"status"` // pending, confirmed, failed
	BlockNumber uint64                 `json:"block_number"`
	CreatedAt   time.Time              `json:"created_at"`
	ConfirmedAt *time.Time             `json:"confirmed_at,omitempty"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// SmartContract represents a smart contract
type SmartContract struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Address    string                 `json:"address"`
	ABI        string                 `json:"abi"`
	Bytecode   string                 `json:"bytecode"`
	Network    string                 `json:"network"`
	DeployedBy string                 `json:"deployed_by"`
	CreatedAt  time.Time              `json:"created_at"`
	IsActive   bool                   `json:"is_active"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// ContractCall represents a smart contract function call
type ContractCall struct {
	ID           string     `json:"id"`
	ContractID   string     `json:"contract_id"`
	FunctionName string     `json:"function_name"`
	Parameters   []string   `json:"parameters"`
	Value        string     `json:"value"`
	GasLimit     string     `json:"gas_limit"`
	TxHash       string     `json:"tx_hash"`
	Status       string     `json:"status"`
	Result       string     `json:"result"`
	CreatedAt    time.Time  `json:"created_at"`
	ExecutedAt   *time.Time `json:"executed_at,omitempty"`
}

// WalletManager manages blockchain wallets
type WalletManager struct {
	mu           sync.RWMutex
	client       *client.Client
	configDir    string
	wallets      map[string]*Wallet
	transactions map[string]*Transaction
	stats        *BlockchainStats
}

// BlockchainStats represents blockchain statistics
type BlockchainStats struct {
	TotalWallets      int       `json:"total_wallets"`
	TotalTransactions int       `json:"total_transactions"`
	TotalContracts    int       `json:"total_contracts"`
	TotalVolume       string    `json:"total_volume"`
	ActiveWallets     int       `json:"active_wallets"`
	PendingTxs        int       `json:"pending_transactions"`
	LastUpdated       time.Time `json:"last_updated"`
}

// NewWalletManager creates a new wallet manager
func NewWalletManager(client *client.Client, configDir string) *WalletManager {
	wm := &WalletManager{
		client:       client,
		configDir:    configDir,
		wallets:      make(map[string]*Wallet),
		transactions: make(map[string]*Transaction),
		stats:        &BlockchainStats{},
	}
	_ = wm.loadWallets()      // Ignore error for initialization
	_ = wm.loadTransactions() // Ignore error for initialization
	_ = wm.loadStats()        // Ignore error for initialization
	return wm
}

// CreateWallet creates a new blockchain wallet
func (wm *WalletManager) CreateWallet(ctx context.Context, name, currency, network string) (*Wallet, error) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Generate new private key
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Get public key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("failed to cast public key to ECDSA")
	}

	// Get address
	address := crypto.PubkeyToAddress(*publicKeyECDSA).Hex()

	wallet := &Wallet{
		ID:         uuid.New().String(),
		Name:       name,
		Address:    address,
		PrivateKey: hex.EncodeToString(crypto.FromECDSA(privateKey)),
		PublicKey:  hex.EncodeToString(crypto.FromECDSAPub(publicKeyECDSA)),
		Balance:    "0",
		Currency:   currency,
		Network:    network,
		CreatedAt:  time.Now(),
		LastUsed:   time.Now(),
		IsActive:   true,
		Metadata:   make(map[string]interface{}),
	}

	wm.wallets[wallet.ID] = wallet

	// Simulate API call - store wallet data as JSON
	stored, err := sealedWalletCopy(wallet)
	if err != nil {
		return nil, err
	}
	walletData, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal wallet: %v", err)
	}

	tempFilePath := filepath.Join(wm.configDir, fmt.Sprintf("wallets/%s.json", wallet.ID))
	if err := os.WriteFile(tempFilePath, walletData, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write wallet data to temp file: %v", err)
	}
	defer func() { _ = os.Remove(tempFilePath) }() // Clean up temp file

	_, err = wm.client.StoreFile(ctx, tempFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to store wallet: %v", err)
	}

	wm.stats.TotalWallets++
	wm.stats.ActiveWallets++
	_ = wm.saveStats()
	_ = wm.saveWallets()
	return wallet, nil
}

// ListWallets returns all wallets
func (wm *WalletManager) ListWallets(ctx context.Context) ([]*Wallet, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	wallets := make([]*Wallet, 0, len(wm.wallets))
	for _, wallet := range wm.wallets {
		if wallet.IsActive {
			wallets = append(wallets, wallet)
		}
	}
	return wallets, nil
}

// GetWallet returns a wallet by ID
func (wm *WalletManager) GetWallet(ctx context.Context, walletID string) (*Wallet, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	wallet, exists := wm.wallets[walletID]
	if !exists {
		return nil, fmt.Errorf("wallet not found: %s", walletID)
	}
	return wallet, nil
}

// GetWalletByAddress returns a wallet by address
func (wm *WalletManager) GetWalletByAddress(ctx context.Context, address string) (*Wallet, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	for _, wallet := range wm.wallets {
		if wallet.Address == address && wallet.IsActive {
			return wallet, nil
		}
	}
	return nil, fmt.Errorf("wallet not found for address: %s", address)
}

// UpdateWalletBalance updates wallet balance
func (wm *WalletManager) UpdateWalletBalance(ctx context.Context, walletID, balance string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	wallet, exists := wm.wallets[walletID]
	if !exists {
		return fmt.Errorf("wallet not found: %s", walletID)
	}

	wallet.Balance = balance
	wallet.LastUsed = time.Now()

	_ = wm.saveWallets()
	return nil
}

// CreateTransaction creates a new transaction
func (wm *WalletManager) CreateTransaction(ctx context.Context, from, to, amount, currency, gasPrice, gasLimit string) (*Transaction, error) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	transaction := &Transaction{
		ID:        uuid.New().String(),
		From:      from,
		To:        to,
		Amount:    amount,
		Currency:  currency,
		GasPrice:  gasPrice,
		GasLimit:  gasLimit,
		Nonce:     uint64(time.Now().UnixNano()),
		Hash:      fmt.Sprintf("0x%x", uuid.New()),
		Status:    "pending",
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	wm.transactions[transaction.ID] = transaction

	// Simulate API call - store transaction data as JSON
	txData, err := json.Marshal(transaction)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction: %v", err)
	}

	tempFilePath := filepath.Join(wm.configDir, fmt.Sprintf("transactions/%s.json", transaction.ID))
	if err := os.WriteFile(tempFilePath, txData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write transaction data to temp file: %v", err)
	}
	defer func() { _ = os.Remove(tempFilePath) }() // Clean up temp file

	_, err = wm.client.StoreFile(ctx, tempFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to store transaction: %v", err)
	}

	wm.stats.TotalTransactions++
	wm.stats.PendingTxs++
	_ = wm.saveStats()
	_ = wm.saveTransactions()
	return transaction, nil
}

// ListTransactions returns all transactions
func (wm *WalletManager) ListTransactions(ctx context.Context) ([]*Transaction, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	transactions := make([]*Transaction, 0, len(wm.transactions))
	for _, tx := range wm.transactions {
		transactions = append(transactions, tx)
	}
	return transactions, nil
}

// GetTransaction returns a transaction by ID
func (wm *WalletManager) GetTransaction(ctx context.Context, txID string) (*Transaction, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	transaction, exists := wm.transactions[txID]
	if !exists {
		return nil, fmt.Errorf("transaction not found: %s", txID)
	}
	return transaction, nil
}

// UpdateTransactionStatus updates transaction status
func (wm *WalletManager) UpdateTransactionStatus(ctx context.Context, txID, status string, blockNumber uint64) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	transaction, exists := wm.transactions[txID]
	if !exists {
		return fmt.Errorf("transaction not found: %s", txID)
	}

	transaction.Status = status
	transaction.BlockNumber = blockNumber
	if status == "confirmed" {
		now := time.Now()
		transaction.ConfirmedAt = &now
		wm.stats.PendingTxs--
	}

	_ = wm.saveTransactions()
	_ = wm.saveStats()
	return nil
}

// GetBlockchainStats returns blockchain statistics
func (wm *WalletManager) GetBlockchainStats(ctx context.Context) (*BlockchainStats, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	// Update stats
	wm.stats.LastUpdated = time.Now()
	return wm.stats, nil
}

// File operations
func (wm *WalletManager) loadWallets() error {
	walletsFile := filepath.Join(wm.configDir, "wallets.json")
	if _, err := os.Stat(walletsFile); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(walletsFile)
	if err != nil {
		return fmt.Errorf("failed to read wallets file: %w", err)
	}

	var wallets []*Wallet
	if err := json.Unmarshal(data, &wallets); err != nil {
		return fmt.Errorf("failed to unmarshal wallets: %w", err)
	}

	for _, wallet := range wallets {
		plain, err := shugocrypto.OpenString(wallet.PrivateKey)
		if err != nil {
			return err
		}
		wallet.PrivateKey = plain
		wm.wallets[wallet.ID] = wallet
	}
	return nil
}

func (wm *WalletManager) saveWallets() error {
	walletsFile := filepath.Join(wm.configDir, "wallets.json")

	var wallets []*Wallet
	for _, wallet := range wm.wallets {
		stored, err := sealedWalletCopy(wallet)
		if err != nil {
			return err
		}
		wallets = append(wallets, stored)
	}

	data, err := json.MarshalIndent(wallets, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal wallets: %w", err)
	}

	return os.WriteFile(walletsFile, data, 0o600)
}

func (wm *WalletManager) loadTransactions() error {
	transactionsFile := filepath.Join(wm.configDir, "transactions.json")
	if _, err := os.Stat(transactionsFile); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(transactionsFile)
	if err != nil {
		return fmt.Errorf("failed to read transactions file: %w", err)
	}

	var transactions []*Transaction
	if err := json.Unmarshal(data, &transactions); err != nil {
		return fmt.Errorf("failed to unmarshal transactions: %w", err)
	}

	for _, tx := range transactions {
		wm.transactions[tx.ID] = tx
	}
	return nil
}

func (wm *WalletManager) saveTransactions() error {
	transactionsFile := filepath.Join(wm.configDir, "transactions.json")

	var transactions []*Transaction
	for _, tx := range wm.transactions {
		transactions = append(transactions, tx)
	}

	data, err := json.MarshalIndent(transactions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal transactions: %w", err)
	}

	return os.WriteFile(transactionsFile, data, 0644)
}

func (wm *WalletManager) loadStats() error {
	statsFile := filepath.Join(wm.configDir, "blockchain_stats.json")
	if _, err := os.Stat(statsFile); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(statsFile)
	if err != nil {
		return fmt.Errorf("failed to read stats file: %w", err)
	}

	var stats BlockchainStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return fmt.Errorf("failed to unmarshal stats: %w", err)
	}

	wm.stats = &stats
	return nil
}

func (wm *WalletManager) saveStats() error {
	statsFile := filepath.Join(wm.configDir, "blockchain_stats.json")

	data, err := json.MarshalIndent(wm.stats, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}

	return os.WriteFile(statsFile, data, 0644)
}

func sealedWalletCopy(wallet *Wallet) (*Wallet, error) {
	stored := *wallet
	sealed, err := shugocrypto.SealString(wallet.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt wallet key: %w", err)
	}
	stored.PrivateKey = sealed
	return &stored, nil
}
