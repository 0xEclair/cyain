package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"time"
	
	"github.com/boltdb/bolt"
)

const (
	dbFile              = "blockchain.db"
	blocksBucket        = "blocks"
	genesisCoinbaseData = "The Times 03/Jan/2009 Chancellor on brink of second bailout for banks"
)

type Block struct {
	Timestamp     int64
	Transactions  []*Transaction
	PrevBlockHash []byte
	Hash          []byte
	Nonce         int
}

func NewBlock(trasnactions []*Transaction, prevBlockHash []byte) *Block {
	block := &Block{
		time.Now().Unix(),
		trasnactions,
		prevBlockHash,
		[]byte{},
		0,
	}
	
	pow := NewProofOfWork(block)
	nonce, hash := pow.Run()
	
	block.Hash = hash
	block.Nonce = nonce
	return block
}

type BlockChain struct {
	tip []byte
	db  *bolt.DB
}

func (bc *BlockChain) MineBlock(transactions []*Transaction) {
	var lastHash []byte
	
	for _, tx := range transactions {
		if bc.VerifyTransaction(tx) != true {
			log.Panic("ERROR: invalid transaction")
		}
	}
	
	viewf := func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		lastHash = b.Get([]byte("l"))
		return nil
	}
	err := bc.db.View(viewf)
	
	if err != nil {
		log.Panic(err)
	}
	
	newBlock := NewBlock(transactions, lastHash)
	
	updatef := func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		
		err := b.Put(newBlock.Hash, newBlock.SerializeBlock())
		if err != nil {
			log.Panic(err)
		}
		
		err = b.Put([]byte("l"), newBlock.Hash)
		if err != nil {
			log.Panic(err)
		}
		
		bc.tip = newBlock.Hash
		
		return nil
	}
	
	err = bc.db.Update(updatef)
}

func NewGenesisBlock(coinbase *Transaction) *Block {
	return NewBlock([]*Transaction{coinbase}, []byte{})
}

func NewBlockchain(address string) *BlockChain {
	if dbExists() == false {
		fmt.Println("No existing blockchain found. Create one first.")
		os.Exit(1)
	}
	var tip []byte
	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}
	
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		tip = b.Get([]byte("l"))
		
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	
	bc := BlockChain{
		tip,
		db,
	}
	return &bc
}

func dbExists() bool {
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		return false
	}
	return true
}

func CreateBlockchain(address string) *BlockChain {
	if dbExists() {
		fmt.Println("Blockchain already exists.")
		os.Exit(254)
	}
	
	var tip []byte
	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}
	
	err = db.Update(func(tx *bolt.Tx) error {
		cbtx := NewCoinbaseTx(address, genesisCoinbaseData)
		genesis := NewGenesisBlock(cbtx)
		
		b, err := tx.CreateBucket([]byte(blocksBucket))
		if err != nil {
			log.Panic(err)
		}
		
		err = b.Put(genesis.Hash, genesis.SerializeBlock())
		if err != nil {
			log.Panic(err)
		}
		
		err = b.Put([]byte("l"), genesis.Hash)
		if err != nil {
			log.Panic(err)
		}
		tip = genesis.Hash
		
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	
	bc := BlockChain{tip, db}
	
	return &bc
}

func (b *Block) SerializeBlock() []byte {
	var result bytes.Buffer
	
	encoder := gob.NewEncoder(&result)
	err := encoder.Encode(b)
	_ = err
	
	return result.Bytes()
}

func (b *Block) HashTransaction() []byte {
	var txHashes [][]byte
	var txHash [32]byte
	
	for _, tx := range b.Transactions {
		txHashes = append(txHashes, tx.ID)
	}
	txHash = sha256.Sum256(bytes.Join(txHashes, []byte{}))
	
	return txHash[:]
}

func DeserializeBlock(data []byte) *Block {
	var block Block
	
	decoder := gob.NewDecoder(bytes.NewReader(data))
	err := decoder.Decode(&block)
	_ = err
	
	return &block
}

type BlockchainIterator struct {
	currentHash []byte
	db          *bolt.DB
}

func (bc *BlockChain) Iterator() *BlockchainIterator {
	bci := &BlockchainIterator{
		bc.tip,
		bc.db,
	}
	
	return bci
}

func (i *BlockchainIterator) Next() *Block {
	var block *Block
	
	err := i.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		encodedBlock := b.Get(i.currentHash)
		block = DeserializeBlock(encodedBlock)
		
		return nil
	})
	
	i.currentHash = block.PrevBlockHash
	
	_ = err
	return block
}

func (bc *BlockChain) FindUnspentTransactions(pubKeyHash []byte) []Transaction {
	var unspentTxs []Transaction
	spentTxOs := make(map[string][]int)
	bci := bc.Iterator()
	
	for {
		block := bci.Next()
		for _, tx := range block.Transactions {
			txID := hex.EncodeToString(tx.ID)
		
		Outputs:
			for outIdx, out := range tx.Vout {
				if spentTxOs[txID] != nil {
					for _, spentOut := range spentTxOs[txID] {
						if spentOut == outIdx {
							continue Outputs
						}
					}
				}
				
				if out.IsLockedWithKey(pubKeyHash) {
					unspentTxs = append(unspentTxs, *tx)
				}
			}
			
			// coinbase tx没有input
			if tx.IsCoinbase() == false {
				for _, in := range tx.Vin {
					if in.UsesKey(pubKeyHash) {
						inTxID := hex.EncodeToString(in.Txid)
						spentTxOs[inTxID] = append(spentTxOs[inTxID], in.Vout)
					}
				}
			}
		}
		// genesis block -> break
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	return unspentTxs
}

func (bc *BlockChain) FindSpendableOutputs(pubKeyHash []byte, amount int) (int, map[string][]int) {
	unspentOutputs := make(map[string][]int)
	unspentTxs := bc.FindUnspentTransactions(pubKeyHash)
	accumulated := 0

Work:
	for _, tx := range unspentTxs {
		txID := hex.EncodeToString(tx.ID)
		for outIdx, out := range tx.Vout {
			if out.IsLockedWithKey(pubKeyHash) && accumulated < amount {
				accumulated += out.Value
				unspentOutputs[txID] = append(unspentOutputs[txID], outIdx)
				
				if accumulated >= amount {
					// still run the code
					// would return accumulated, unspentOutputs;
					break Work
				}
			}
		}
	}
	
	return accumulated, unspentOutputs
}

func (bc *BlockChain) FindUTXO(pubKeyHash []byte) []TxOutput {
	var UTXOs []TxOutput
	unspentTransactions := bc.FindUnspentTransactions(pubKeyHash)
	
	for _, tx := range unspentTransactions {
		for _, out := range tx.Vout {
			if out.IsLockedWithKey(pubKeyHash) {
				UTXOs = append(UTXOs, out)
			}
		}
	}
	
	return UTXOs
}

func (bc *BlockChain) FindTransaction(ID []byte) (Transaction, error) {
	bci := bc.Iterator()
	
	for {
		block := bci.Next()
		
		for _, tx := range block.Transactions {
			if bytes.Compare(tx.ID, ID) == 0 {
				return *tx, nil
			}
		}
		
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	
	return Transaction{}, errors.New("Transaction is not found")
}

func (bc *BlockChain) SignTransaction(tx *Transaction, privKey ecdsa.PrivateKey) {
	prevTXs := make(map[string]Transaction)
	
	for _, vin := range tx.Vin {
		prevTX, _ := bc.FindTransaction(vin.Txid)
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}
	
	tx.Sign(privKey, prevTXs)
}

func (bc *BlockChain) VerifyTransaction(tx *Transaction) bool {
	prevTXs := make(map[string]Transaction)
	
	for _, vin := range tx.Vin {
		prevTX, _ := bc.FindTransaction(vin.Txid)
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}
	
	return tx.Verify(prevTXs)
}

func (u UTXOSet) FindUTXO(pubKeyHash []byte) []TxOutput {
	var UTXOs []TxOutput
	db := u.BlockChain.db
	
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket))
		c := b.Cursor()
		
		for k, v := c.First(); k != nil; k, v = c.Next() {
			outs := DeserializeOutputs(v)
			
			for _, out := range outs.Outputs {
				if out.IsLockedWithKey(pubKeyHash) {
					UTXOs = append(UTXOs, out)
				}
			}
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	return UTXOs
}

func (u UTXOSet) Update(block *Block) {
	db := u.BlockChain.db
	
	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket))
		
		for _, tx := range block.Transactions {
			if tx.IsCoinbase() == false {
				for _, vin := range tx.Vin {
					updatedOuts := TxOutputs{}
					outsBytes := b.Get(vin.Txid)
					outs := DeserializeOutputs(outsBytes)
					
					for outIdx, out := range outs.Outputs {
						if outIdx != vin.Vout {
							updatedOuts.Outputs = append(updatedOuts.Outputs, out)
						}
					}
					
					if len(updatedOuts.Outputs) == 0 {
						err := b.Delete(vin.Txid)
						if err != nil {
							log.Panic(err)
						}
					} else {
						err := b.Put(vin.Txid, updatedOuts.Serialize())
						if err != nil {
							log.Panic(err)
						}
					}
				}
			}
			
			newOutputs := TxOutputs{}
			for _, out := range tx.Vout {
				newOutputs.Outputs = append(newOutputs.Outputs, out)
			}
			err := b.Put(tx.ID, newOutputs.Serialize())
			if err != nil {
				log.Panic(err)
			}
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
}
