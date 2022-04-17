package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"strconv"
	"time"
	
	"github.com/boltdb/bolt"
)

const (
	dbFile       = "blockchain.db"
	blocksBucket = "blocks"
)

type Block struct {
	Timestamp     int64
	Transactions  []*Transaction
	Data          []byte
	PrevBlockHash []byte
	Hash          []byte
	Nonce         int
}

func (b *Block) SetHash() {
	timestamp := []byte(strconv.FormatInt(b.Timestamp, 10))
	headers := bytes.Join([][]byte{
		b.PrevBlockHash,
		b.Data,
		timestamp,
	}, []byte{})
	hash := sha256.Sum256(headers)
	
	b.Hash = hash[:]
}

func NewBlock(data string, prevBlockHash []byte) *Block {
	block := &Block{
		time.Now().Unix(),
		nil,
		[]byte(data),
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

func (bc *BlockChain) AddBlock(data string) {
	var lastHash []byte
	
	viewf := func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		lastHash = b.Get([]byte("l"))
		return nil
	}
	_ = bc.db.View(viewf)
	
	newBlock := NewBlock(data, lastHash)
	
	updatef := func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		err := b.Put(newBlock.Hash, newBlock.SerializeBlock())
		err = b.Put([]byte("l"), newBlock.Hash)
		bc.tip = newBlock.Hash
		
		_ = err
		return nil
	}
	
	_ = bc.db.Update(updatef)
}

func NewGenesisBlock() *Block {
	return NewBlock("Genesis Block", []byte{})
}

func NewBlockchain() *BlockChain {
	var tip []byte
	db, err := bolt.Open(dbFile, 0600, nil)
	
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blocksBucket))
		
		if b == nil {
			genesis := NewGenesisBlock()
			b, _ := tx.CreateBucket([]byte(blocksBucket))
			err = b.Put(genesis.Hash, genesis.SerializeBlock())
			err = b.Put([]byte("l"), genesis.Hash)
			tip = genesis.Hash
		} else {
			tip = b.Get([]byte("l"))
		}
		
		return nil
	})
	
	bc := BlockChain{
		tip,
		db,
	}
	
	_ = err
	return &bc
}

func (b *Block) SerializeBlock() []byte {
	var result bytes.Buffer
	
	encoder := gob.NewEncoder(&result)
	err := encoder.Encode(b)
	_ = err
	
	return result.Bytes()
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

func (bc *BlockChain) FindUnspentTransactions(address string) []Transaction {
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
				
				if out.CanBeUnlockedWith(address) {
					unspentTxs = append(unspentTxs, *tx)
				}
			}
			
			if tx.IsCoinbase() == false {
				for _, in := range tx.Vin {
					if in.CanUnlockOutputWith(address) {
						inTxID := hex.EncodeToString(in.Txid)
						spentTxOs[inTxID] = append(spentTxOs[inTxID], in.Vout)
					}
				}
			}
		}
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}
	return unspentTxs
}

func (bc *BlockChain) FindSpendableOutputs(address string, amount int) (int, map[string][]int) {
	unspentOutputs := make(map[string][]int)
	unspentTxs := bc.FindUnspentTransactions(address)
	accumulated := 0

Work:
	for _, tx := range unspentTxs {
		txID := hex.EncodeToString(tx.ID)
		for outIdx, out := range tx.Vout {
			if out.CanBeUnlockedWith(address) && accumulated < amount {
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
