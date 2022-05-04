package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"github.com/boltdb/bolt"
	"log"
	"math/big"
	
	"cyain/utils"
)

// reward for mining
const subsidy = 10
const utxoBucket = "chainstate"

type Transaction struct {
	ID   []byte
	Vin  []TxInput
	Vout []TxOutput
}

func (tx Transaction) IsCoinbase() bool {
	return len(tx.Vin) == 1 && len(tx.Vin[0].Txid) == 0 && tx.Vin[0].Vout == -1
}

func (tx *Transaction) SetID() {
	var encoded bytes.Buffer
	var hash [32]byte
	
	enc := gob.NewEncoder(&encoded)
	err := enc.Encode(tx)
	if err != nil {
		log.Panic(err)
	}
	hash = sha256.Sum256(encoded.Bytes())
	tx.ID = hash[:]
}

type TxInput struct {
	Txid      []byte
	Vout      int
	Signature []byte
	PubKey    []byte
}

func (in *TxInput) UsesKey(pubKeyHash []byte) bool {
	lockingHash := HashPubKey(in.PubKey)
	
	return bytes.Compare(lockingHash, pubKeyHash) == 0
}

type TxOutput struct {
	Value      int
	PubKeyHash []byte
}

func (out *TxOutput) Lock(address []byte) {
	pubKeyHash := utils.Base58Decode(address)
	pubKeyHash = pubKeyHash[1 : len(pubKeyHash)-4]
	out.PubKeyHash = pubKeyHash
}

func (out *TxOutput) IsLockedWithKey(pubKeyHash []byte) bool {
	return bytes.Compare(out.PubKeyHash, pubKeyHash) == 0
}

type TxOutputs struct {
	Outputs []TxOutput
}

func (outs TxOutputs) Serialize() []byte {
	var buff bytes.Buffer
	
	enc := gob.NewEncoder(&buff)
	err := enc.Encode(outs)
	if err != nil {
		log.Panic(err)
	}
	return buff.Bytes()
}

func DeserializeOutputs(data []byte) TxOutputs {
	var outputs TxOutputs
	
	dec := gob.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(&outputs)
	if err != nil {
		log.Panic(err)
	}
	return outputs
}

func NewTxOutput(value int, address string) *TxOutput {
	txo := &TxOutput{
		value,
		nil,
	}
	txo.Lock([]byte(address))
	
	return txo
}

func NewUTXOTransaction(from, to string, amount int, bc *BlockChain) *Transaction {
	var inputs []TxInput
	var outputs []TxOutput
	
	wallets, err := NewWallets()
	if err != nil {
		log.Panic(err)
	}
	wallet := wallets.GetWallet(from)
	pubKeyHash := HashPubKey(wallet.PublicKey)
	acc, validOutputs := bc.FindSpendableOutputs(pubKeyHash, amount)
	
	if acc < amount {
		log.Panic("ERROR: Not enough funds")
	}
	
	for txid, outs := range validOutputs {
		txID, _ := hex.DecodeString(txid)
		for _, out := range outs {
			// 引用了之前的out
			input := TxInput{
				txID,
				out,
				nil,
				wallet.PublicKey,
			}
			inputs = append(inputs, input)
		}
	}
	
	outputs = append(outputs, *NewTxOutput(amount, to))
	
	if acc > amount {
		outputs = append(outputs, *NewTxOutput(acc-amount, from))
	}
	
	tx := Transaction{nil, inputs, outputs}
	tx.ID = tx.Hash()
	bc.SignTransaction(&tx, wallet.PrivateKey)
	
	return &tx
}

// coinbase -> input是0，但是有output的tx
func NewCoinbaseTx(to, data string) *Transaction {
	if data == "" {
		data = fmt.Sprintf("Reward to '%s'", to)
	}
	
	txin := TxInput{
		[]byte{},
		-1,
		nil,
		[]byte(data),
	}
	txout := NewTxOutput(
		subsidy,
		to,
	)
	tx := Transaction{
		nil,
		[]TxInput{txin},
		[]TxOutput{*txout},
	}
	tx.SetID()
	
	return &tx
}

func (tx Transaction) Serialize() []byte {
	var encoded bytes.Buffer
	
	enc := gob.NewEncoder(&encoded)
	err := enc.Encode(tx)
	
	if err != nil {
		log.Panic(err)
	}
	
	return encoded.Bytes()
}

func (tx *Transaction) Hash() []byte {
	var hash [32]byte
	
	txcopy := *tx
	txcopy.ID = []byte{}
	
	hash = sha256.Sum256(txcopy.Serialize())
	
	return hash[:]
}

func (tx *Transaction) Sign(privKey ecdsa.PrivateKey, prevTxs map[string]Transaction) {
	if tx.IsCoinbase() {
		return
	}
	
	txCopy := tx.TrimmedCopy()
	
	for inID, vin := range txCopy.Vin {
		prevTx := prevTxs[hex.EncodeToString(vin.Txid)]
		txCopy.Vin[inID].Signature = nil
		// vout 作为int只是为了表示在tx中，这个out的序号
		txCopy.Vin[inID].PubKey = prevTx.Vout[vin.Vout].PubKeyHash
		txCopy.ID = txCopy.Hash()
		txCopy.Vin[inID].PubKey = nil
		
		r, s, err := ecdsa.Sign(rand.Reader, &privKey, txCopy.ID)
		signature := append(r.Bytes(), s.Bytes()...)
		
		tx.Vin[inID].Signature = signature
		_ = err
	}
}

func (tx *Transaction) TrimmedCopy() Transaction {
	var inputs []TxInput
	var outputs []TxOutput
	
	for _, vin := range tx.Vin {
		inputs = append(inputs, TxInput{vin.Txid, vin.Vout, nil, nil})
	}
	
	for _, vout := range tx.Vout {
		outputs = append(outputs, TxOutput{vout.Value, vout.PubKeyHash})
	}
	
	txCopy := Transaction{tx.ID, inputs, outputs}
	
	return txCopy
}

func (tx *Transaction) Verify(prevTxs map[string]Transaction) bool {
	txCopy := tx.TrimmedCopy()
	curve := elliptic.P256()
	
	for inID, vin := range tx.Vin {
		prevTx := prevTxs[hex.EncodeToString(vin.Txid)]
		txCopy.Vin[inID].Signature = nil
		txCopy.Vin[inID].PubKey = prevTx.Vout[vin.Vout].PubKeyHash
		txCopy.ID = txCopy.Hash()
		txCopy.Vin[inID].PubKey = nil
		
		r := big.Int{}
		s := big.Int{}
		sigLen := len(vin.Signature)
		r.SetBytes(vin.Signature[:(sigLen / 2)])
		s.SetBytes(vin.Signature[(sigLen / 2):])
		
		x := big.Int{}
		y := big.Int{}
		keyLen := len(vin.PubKey)
		x.SetBytes(vin.PubKey[:(keyLen / 2)])
		y.SetBytes(vin.PubKey[(keyLen / 2):])
		
		rawPubKey := ecdsa.PublicKey{curve, &x, &y}
		if ecdsa.Verify(&rawPubKey, txCopy.ID, &r, &s) == false {
			return false
		}
	}
	
	return true
}

type UTXOSet struct {
	BlockChain *BlockChain
}

func (u UTXOSet) Reindex() {
	db := u.BlockChain.db
	bucketName := []byte(utxoBucket)
	
	err := db.Update(func(tx *bolt.Tx) error {
		err := tx.DeleteBucket(bucketName)
		if err != nil && err != bolt.ErrBucketNotFound {
			log.Panic(err)
		}
		
		_, err = tx.CreateBucket(bucketName)
		if err != nil {
			log.Panic(err)
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	UTXO := u.BlockChain.FindUTXO()
	
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		
		for txID, outs := range UTXO {
			key, err := hex.DecodeString(txID)
			if err != nil {
				log.Panic(err)
			}
			err = b.Put(key, outs.Serialize())
			if err != nil {
				log.Panic(err)
			}
			return nil
		}
	})
}

func (u UTXOSet) FindSpendableOutputs(pubkeyHash []byte, amount int) (int, map[string][]int) {
	unspentOutputs := make(map[string][]int)
	accumulated := 0
	db := u.BlockChain.db
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(utxoBucket))
		c := b.Cursor()
		
		for k, v := c.First(); k != nil; k, v = c.Next() {
			txID := hex.EncodeToString(k)
			outs := DeserializeOutputs(v)
			
			for outIdx, out := range outs.Outputs {
				if out.IsLockedWithKey(pubkeyHash) && accumulated < amount {
					accumulated += out.Value
					unspentOutputs[txID] = append(unspentOutputs[txID], outIdx)
				}
			}
		}
		return nil
	})
	if err != nil {
		log.Panic(err)
	}
	
	return accumulated, unspentOutputs
}
