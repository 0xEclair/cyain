package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
)

const subsidy = 10

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
	ScriptSig string
}

func (in *TxInput) CanUnlockOutputWith(unlockingData string) bool {
	return in.ScriptSig == unlockingData
}

type TxOutput struct {
	Value        int
	ScriptPubKey string
}

func (out *TxOutput) CanBeUnlockedWith(unlockingData string) bool {
	return out.ScriptPubKey == unlockingData
}

func NewUTXOTransaction(from, to string, amount int, bc *BlockChain) *Transaction {
	var inputs []TxInput
	var outputs []TxOutput
	
	acc, validOutputs := bc.FindSpendableOutputs(from, amount)
	
	if acc < amount {
		log.Panic("ERROR: Not enough funds")
	}
	
	for txid, outs := range validOutputs {
		txID, _ := hex.DecodeString(txid)
		for _, out := range outs {
			input := TxInput{
				txID,
				out,
				from,
			}
			inputs = append(inputs, input)
		}
	}
	
	outputs = append(outputs, TxOutput{amount, to})
	
	if acc > amount {
		outputs = append(outputs, TxOutput{acc - amount, from})
	}
	
	tx := Transaction{nil, inputs, outputs}
	tx.SetID()
	
	return &tx
}

func NewCoinbaseTx(to, data string) *Transaction {
	if data == "" {
		data = fmt.Sprintf("Reward to '%s'", to)
	}
	
	txin := TxInput{
		[]byte{},
		-1,
		data,
	}
	txout := TxOutput{
		subsidy,
		to,
	}
	tx := Transaction{
		nil,
		[]TxInput{txin},
		[]TxOutput{txout},
	}
	tx.SetID()
	
	return &tx
}
