package main

import (
	"encoding/hex"
	"log"
)

type Transaction struct {
	ID   []byte
	Vin  []TxInput
	Vout []TxOutput
}

type TxInput struct {
	Txid      []byte
	Vout      int
	ScriptSig string
}

type TxOutput struct {
	Value        int
	ScriptPubKey string
}

func NewUTXOTransaction(from, to string, amount int, bc *BlockChain) *Transaction {
	var inputs []TxInput
	var outputs []TxOutput
	
	acc, validOutputs := bc.FindSpendableOutputs(from, amount)
	
	if acc < amount {
		log.Panic("ERROR: Not enough funds")
	}
	
	for txid, outs := range validOutputs {
		txID, err := hex.DecodeString(txid)
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
